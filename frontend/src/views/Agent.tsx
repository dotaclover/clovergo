import { useEffect, useRef, useState, type FormEvent, type KeyboardEvent } from "react";
import { Bot, Plus, Send, Trash2, Wrench } from "lucide-react";
import { postJSON } from "../lib/api";
import { toastError, useApp } from "../lib/context";
import { t } from "../lib/i18n";
import type { AgentMessage, AgentReply } from "../lib/types";
import { CitationCard } from "../components/knowledge";
import { Markdown } from "../components/Markdown";
import { Button, CopyButton, Empty, ErrorNotice, LongTask, cx } from "../components/ui";

const STORE_KEY = "clovergo.chats";
const MAX_CHATS = 50;

interface Turn {
  id: number;
  question: string;
  reply?: AgentReply;
  error?: unknown;
}

interface Chat {
  id: string;
  title: string;
  updated: number;
  turns: Turn[];
}

function loadChats(): Chat[] {
  try {
    const raw = window.localStorage.getItem(STORE_KEY);
    const list = raw ? JSON.parse(raw) : [];
    return Array.isArray(list) ? list.filter((item) => item && typeof item.id === "string" && Array.isArray(item.turns)) : [];
  } catch {
    return [];
  }
}

function saveChats(chats: Chat[]) {
  try {
    // Unanswered turns and errors are transient; only answered turns are kept.
    const clean = chats.slice(0, MAX_CHATS).map((chat) => ({ ...chat, turns: chat.turns.filter((turn) => turn.reply) }));
    window.localStorage.setItem(STORE_KEY, JSON.stringify(clean));
  } catch {
    // storage unavailable or full: the conversation just won't survive a reload
  }
}

/**
 * The agent chat: conversations on the left, the open one on the right.
 * Conversations live in this browser (localStorage) and the open one is sent
 * back in full on every request; the server keeps no session.
 */
export default function AgentView() {
  const { notify } = useApp();
  const [chats, setChats] = useState<Chat[]>(loadChats);
  const [activeId, setActiveId] = useState<string>(() => loadChats()[0]?.id ?? "");
  const [input, setInput] = useState("");
  const [pending, setPending] = useState(false);
  const seq = useRef(Date.now());
  const bottom = useRef<HTMLDivElement>(null);

  const active = chats.find((chat) => chat.id === activeId);
  const turns = active?.turns ?? [];

  useEffect(() => {
    saveChats(chats);
  }, [chats]);

  useEffect(() => {
    bottom.current?.scrollIntoView({ block: "end" });
  }, [activeId, turns.length, pending]);

  const newChat = () => {
    if (pending) return;
    setActiveId("");
    setInput("");
  };

  const removeChat = (id: string) => {
    if (!window.confirm(t("agent.confirmDeleteChat"))) return;
    const rest = chats.filter((chat) => chat.id !== id);
    setChats(rest);
    if (id === activeId) setActiveId(rest[0]?.id ?? "");
  };

  const send = async (event?: FormEvent) => {
    event?.preventDefault();
    const text = input.trim();
    if (!text || pending) return;
    const history: AgentMessage[] = [];
    for (const turn of turns) {
      if (!turn.reply) continue;
      history.push({ role: "user", content: turn.question }, { role: "assistant", content: turn.reply.reply });
    }
    history.push({ role: "user", content: text });

    const turnId = ++seq.current;
    let chatId = activeId;
    if (!active) {
      chatId = `c${++seq.current}`;
      setChats((items) => [{ id: chatId, title: text.slice(0, 40), updated: Date.now(), turns: [{ id: turnId, question: text }] }, ...items]);
      setActiveId(chatId);
    } else {
      setChats((items) => items.map((chat) => (chat.id === chatId ? { ...chat, turns: [...chat.turns, { id: turnId, question: text }] } : chat)));
    }
    setInput("");
    setPending(true);

    const patch = (change: Partial<Turn>) =>
      setChats((items) =>
        items
          .map((chat) =>
            chat.id === chatId ? { ...chat, updated: Date.now(), turns: chat.turns.map((turn) => (turn.id === turnId ? { ...turn, ...change } : turn)) } : chat,
          )
          .sort((a, b) => b.updated - a.updated),
      );
    try {
      const reply = await postJSON<AgentReply>("/api/v1/agent/chat", { messages: history.slice(-19) });
      patch({ reply });
    } catch (error) {
      patch({ error });
      toastError(notify, error);
    } finally {
      setPending(false);
    }
  };

  const onKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key === "Enter" && !event.shiftKey && !event.nativeEvent.isComposing) {
      event.preventDefault();
      void send();
    }
  };

  return (
    <div className="grid gap-3 lg:h-[calc(100vh-6.5rem)] lg:grid-cols-[15rem_minmax(0,1fr)]">
      <aside className="flex max-h-44 min-h-0 flex-col rounded-xl border border-zinc-200 bg-white lg:max-h-none dark:border-zinc-800 dark:bg-zinc-900/40">
        <div className="p-2">
          <Button variant="secondary" icon={Plus} className="w-full" disabled={pending} onClick={newChat}>
            {t("agent.newChat")}
          </Button>
        </div>
        <ul className="scrollbar-thin min-h-0 flex-1 space-y-0.5 overflow-y-auto px-2 pb-2" aria-label={t("agent.chats")}>
          {!chats.length ? <li className="px-2 py-3 text-xs text-zinc-400">{t("agent.noChats")}</li> : null}
          {chats.map((chat) => (
            <li key={chat.id} className="group relative">
              <button
                type="button"
                onClick={() => !pending && setActiveId(chat.id)}
                aria-current={chat.id === activeId ? "true" : undefined}
                className={cx(
                  "block w-full truncate rounded-md py-2 pr-8 pl-2.5 text-left text-[13px] transition",
                  chat.id === activeId
                    ? "bg-emerald-50 font-medium text-emerald-900 dark:bg-emerald-500/10 dark:text-emerald-100"
                    : "text-zinc-600 hover:bg-zinc-100 dark:text-zinc-400 dark:hover:bg-zinc-800/70",
                )}
              >
                {chat.title || t("agent.untitled")}
              </button>
              <button
                type="button"
                aria-label={t("agent.deleteChat")}
                title={t("agent.deleteChat")}
                onClick={() => removeChat(chat.id)}
                className="absolute top-1/2 right-1.5 grid size-6 -translate-y-1/2 place-items-center rounded text-zinc-400 opacity-60 transition hover:bg-zinc-200 hover:text-rose-600 group-hover:opacity-100 dark:hover:bg-zinc-700"
              >
                <Trash2 className="size-3.5" />
              </button>
            </li>
          ))}
        </ul>
      </aside>

      <section className="flex min-h-[28rem] min-w-0 flex-col rounded-xl border border-zinc-200 bg-white lg:min-h-0 dark:border-zinc-800 dark:bg-zinc-900/40">
        <div className="scrollbar-thin min-h-0 flex-1 space-y-6 overflow-y-auto px-4 py-5 sm:px-6">
          {!turns.length ? <Empty icon={Bot} frame={false} title={t("agent.emptyTitle")} description={t("agent.emptyDesc")} /> : null}
          {turns.map((turn) => (
            <TurnView key={turn.id} turn={turn} pending={pending && !turn.reply && !turn.error} />
          ))}
          <div ref={bottom} />
        </div>
        <form onSubmit={send} className="border-t border-zinc-200 p-3 dark:border-zinc-800">
          <div className="flex items-end gap-2">
            <textarea
              value={input}
              onChange={(event) => setInput(event.target.value)}
              onKeyDown={onKeyDown}
              rows={2}
              maxLength={4000}
              placeholder={t("agent.placeholder")}
              className="field-input min-w-0 flex-1 resize-none text-[15px]"
              aria-label={t("agent.input")}
            />
            <Button type="submit" icon={Send} loading={pending} disabled={!input.trim()} aria-label={t("agent.send")}>
              {t("agent.send")}
            </Button>
          </div>
          <p className="mt-1.5 hidden text-xs text-zinc-400 sm:block">{t("agent.shortcut")}</p>
        </form>
      </section>
    </div>
  );
}

function TurnView({ turn, pending }: { turn: Turn; pending: boolean }) {
  const reply = turn.reply;
  const steps = Array.isArray(reply?.steps) ? reply!.steps : [];
  const citations = Array.isArray(reply?.citations) ? reply!.citations : [];
  return (
    <div className="space-y-3">
      <div className="flex justify-end">
        <p className="max-w-[85%] rounded-2xl rounded-br-md bg-emerald-600 px-4 py-2.5 text-sm whitespace-pre-wrap break-words text-white dark:bg-emerald-500 dark:text-emerald-950">
          {turn.question}
        </p>
      </div>
      {pending ? <LongTask active title={t("agent.pending")} /> : null}
      {turn.error ? <ErrorNotice error={turn.error} /> : null}
      {reply ? (
        <div className="flex gap-3">
          <span className="mt-0.5 grid size-7 shrink-0 place-items-center rounded-full bg-emerald-50 text-emerald-700 dark:bg-emerald-500/15 dark:text-emerald-300">
            <Bot className="size-4" />
          </span>
          <div className="min-w-0 flex-1 space-y-3">
            {steps.length ? (
              <details className="text-xs text-zinc-500">
                <summary className="flex cursor-pointer list-none items-center gap-1.5 [&::-webkit-details-marker]:hidden">
                  <Wrench className="size-3.5" />
                  {t("agent.steps", { n: steps.length })}
                </summary>
                <ol className="mt-2 space-y-1">
                  {steps.map((step, index) => (
                    <li key={index} className="break-words">
                      <code className="font-mono text-zinc-700 dark:text-zinc-300">{step.tool}</code> {step.summary}
                      {step.error ? <span className="text-rose-600 dark:text-rose-400"> · {step.error}</span> : null}
                    </li>
                  ))}
                </ol>
              </details>
            ) : null}
            <div className="break-words">
              <Markdown source={reply.reply} />
            </div>
            {citations.length ? (
              <div>
                <p className="mb-2 text-xs font-semibold tracking-wide text-zinc-500 uppercase">{t("agent.citations", { n: citations.length })}</p>
                <div className="grid gap-3 xl:grid-cols-2">
                  {citations.map((citation) => (
                    <CitationCard key={citation.knowledge_id} id={citation.knowledge_id} title={citation.title} evidence={citation.evidence} />
                  ))}
                </div>
              </div>
            ) : null}
            <CopyButton text={reply.reply} />
          </div>
        </div>
      ) : null}
    </div>
  );
}
