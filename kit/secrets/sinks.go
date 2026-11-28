package secrets

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"math/big"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/rubics-code/agent-kit/kit/policy"
)

// Terminal shows a new secret on the operator's terminal, one time. It is for
// a laptop only.
func Terminal(w io.Writer) Sink { return &terminal{w: w} }

type terminal struct {
	mu sync.Mutex
	w  io.Writer
}

func (t *terminal) Where() string { return "the operator's terminal" }

func (t *terminal) Deliver(_ context.Context, label, value string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	_, err := fmt.Fprintf(t.w, "\n  New secret for %s (shown one time, save it now):\n  %s\n\n", label, value)
	return err
}

// Inbox puts a new secret in an Infisical inbox folder, because a container has
// no terminal. The name holds the label and the time, so a second secret never
// replaces the first.
func Inbox(store Store, path string, now func() time.Time) Sink {
	if now == nil {
		now = time.Now
	}
	return &inbox{store: store, path: path, now: now}
}

type inbox struct {
	store Store
	path  string
	now   func() time.Time
}

func (i *inbox) Where() string { return "the Infisical inbox folder " + i.path }

var notName = regexp.MustCompile(`[^A-Z0-9]+`)

// InboxName gives the secret name for a label at a time: LABEL_YYYYMMDD_HHMMSS.
func InboxName(label string, at time.Time) string {
	base := strings.Trim(notName.ReplaceAllString(strings.ToUpper(label), "_"), "_")
	if base == "" {
		base = "SECRET"
	}
	if base[0] < 'A' || base[0] > 'Z' {
		base = "S_" + base
	}
	if len(base) > 110 {
		base = base[:110]
	}
	return base + "_" + at.UTC().Format("20060102_150405")
}

func (i *inbox) Deliver(ctx context.Context, label, value string) error {
	return i.store.Put(ctx, Place{Path: i.path, Name: InboxName(label, i.now())}, value)
}

// ErrNoPlace is the error of a secret with no place to go.
var ErrNoPlace = errors.New("no place for a new secret")

// None is the sink of a container with no secret store. It refuses every secret.
var None Sink = none{}

type none struct{}

func (none) Where() string { return "nowhere" }

func (none) Deliver(_ context.Context, label, _ string) error {
	return fmt.Errorf("%w for %s: the agent runs in a container without Infisical. Call again with storeAt", ErrNoPlace, label)
}

// NoInfisical is the answer to storeAt when the agent has no secret store.
const NoInfisical = "Not done: Infisical is not set up for you, so you cannot use storeAt. Call again without it."

// Ready says, before any approval, whether a new secret has a place to go. A
// tool that makes a secret calls it in Prepare and refuses on an error, so no
// secret is made with no place to put it.
func Ready(sink Sink, store Store, place *Place) error {
	if place != nil {
		if store == nil {
			return errors.New(NoInfisical)
		}
		return nil
	}
	if sink == nil || sink == None {
		return fmt.Errorf("Not done: %w. Call again with storeAt", ErrNoPlace)
	}
	return nil
}

var nameRe = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,127}$`)

// CheckName checks a secret name: upper case with underscores.
func CheckName(name string) error {
	if !nameRe.MatchString(name) {
		return fmt.Errorf("the secret name %q must be upper case with underscores, for example API_SERVER_KEY", name)
	}
	return nil
}

// CheckPlace checks a storeAt place against the agent's writable paths.
func CheckPlace(p policy.Policy, place Place) error {
	if err := CheckName(place.Name); err != nil {
		return err
	}
	if p.Infisical.Writable == nil {
		return errors.New("the policy names no writable Infisical path")
	}
	return p.Infisical.Writable(place.Path)
}

// PlaceText names a place in copy.
func PlaceText(p policy.Policy, place Place) string {
	label := "Infisical"
	if p.Infisical.EnvLabel != nil {
		label += " " + p.Infisical.EnvLabel(place.Path)
	}
	return fmt.Sprintf("%s %s/%s", label, place.Path, place.Name)
}

// DescribePlace is the approval text for a write into the store. It says when
// the write replaces a value or makes a folder.
func DescribePlace(ctx context.Context, p policy.Policy, store Store, place Place) (string, error) {
	folder, exists, err := store.Check(ctx, place)
	if err != nil {
		return "", err
	}
	switch {
	case exists:
		return PlaceText(p, place) + " (REPLACES the current value)", nil
	case !folder:
		return PlaceText(p, place) + " (new, makes the folder " + place.Path + ")", nil
	}
	return PlaceText(p, place) + " (new)", nil
}

// Delivery is where Deliver may put a secret.
type Delivery struct {
	Policy policy.Policy
	Sink   Sink
	Store  Store
	Place  *Place // storeAt, or nil
}

// Deliver puts a new secret where the operator asked, and gives the tool's
// reply, which never holds the value. When the store fails after the secret
// exists, the secret goes to the sink, so that it is not lost.
func Deliver(ctx context.Context, d Delivery, label, value string) (string, error) {
	where := "the operator's terminal"
	if d.Sink != nil {
		where = d.Sink.Where()
	}
	if d.Place == nil || d.Store == nil {
		if d.Sink == nil {
			return "", fmt.Errorf("%w for %s", ErrNoPlace, label)
		}
		if err := d.Sink.Deliver(ctx, label, value); err != nil {
			return "", err
		}
		return fmt.Sprintf("It went directly to %s. You do not have it.", where), nil
	}
	if err := d.Store.Put(ctx, *d.Place, value); err != nil {
		if d.Sink == nil || d.Sink == None {
			return "", fmt.Errorf("storing the secret in Infisical failed (%v), and there is no other place for it", err)
		}
		if serr := d.Sink.Deliver(ctx, label, value); serr != nil {
			return "", serr
		}
		return fmt.Sprintf("Storing it in Infisical failed (%v), so it went to %s instead. You do not have it.", err, where), nil
	}
	return fmt.Sprintf("It is stored in %s. You do not have it.", PlaceText(d.Policy, *d.Place)), nil
}

const (
	upper   = "ABCDEFGHJKLMNPQRSTUVWXYZ"
	lower   = "abcdefghijkmnopqrstuvwxyz"
	digits  = "23456789"
	symbols = "-_.!"
)

// GeneratePassword makes a random password with at least one character of
// each class, and no characters that look alike.
func GeneratePassword(length int) string {
	if length < 8 {
		length = 8
	}
	pick := func(s string) byte {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(s))))
		if err != nil {
			panic(err)
		}
		return s[n.Int64()]
	}
	all := upper + lower + digits + symbols
	b := []byte{pick(upper), pick(lower), pick(digits), pick(symbols)}
	for len(b) < length {
		b = append(b, pick(all))
	}
	for i := len(b) - 1; i > 0; i-- {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		j := n.Int64()
		b[i], b[j] = b[j], b[i]
	}
	return string(b)
}
