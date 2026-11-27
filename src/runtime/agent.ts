// Wires LangGraph. The briefing and the tools come from assembleAgent; this
// file knows nothing about individual groups.
import type { BaseChatModel } from "@langchain/core/language_models/chat_models";
import type { StructuredTool } from "@langchain/core/tools";
import { type BaseCheckpointSaver, MemorySaver } from "@langchain/langgraph";
import { ChatOpenAI } from "@langchain/openai";
import { createAgent, toolCallLimitMiddleware } from "langchain";

export const OPENROUTER_BASE_URL = "https://openrouter.ai/api/v1";

// Upper limit of tool calls in one user turn. It stops a loop.
export const DEFAULT_TOOL_CALL_LIMIT = 25;

export function createAgentGraph(opts: {
  model: BaseChatModel;
  tools: StructuredTool[];
  briefing: string;
  checkpointer?: BaseCheckpointSaver;
  toolCallLimit?: number;
}) {
  return createAgent({
    model: opts.model,
    tools: opts.tools,
    systemPrompt: opts.briefing,
    checkpointer: opts.checkpointer ?? new MemorySaver(),
    middleware: [toolCallLimitMiddleware({ runLimit: opts.toolCallLimit ?? DEFAULT_TOOL_CALL_LIMIT })],
  });
}

export type AgentGraph = ReturnType<typeof createAgentGraph>;

// A chat model on OpenRouter (or any OpenAI-compatible endpoint). `title` is
// the X-Title header, which names the app in OpenRouter's activity list.
export function openRouterModel(opts: { apiKey: string; model: string; title: string; baseUrl?: string }): BaseChatModel {
  return new ChatOpenAI({
    model: opts.model,
    apiKey: opts.apiKey,
    temperature: 0,
    configuration: {
      baseURL: opts.baseUrl ?? OPENROUTER_BASE_URL,
      defaultHeaders: { "X-Title": opts.title },
    },
  });
}
