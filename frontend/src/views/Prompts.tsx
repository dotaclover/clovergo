import { useEffect, useRef, useState, type ReactNode } from "react";
import { Lock, PencilLine, RefreshCw, RotateCcw, ScrollText } from "lucide-react";
import { ApiError, api, postJSON, putJSON } from "../lib/api";
import { toastError, useApp } from "../lib/context";
import { useAsync } from "../lib/hooks";
import { t } from "../lib/i18n";
import { configViewFor } from "../lib/nav";
import type { PromptLayer, PromptLayerEdit, PromptView } from "../lib/types";
import { Badge, Button, Card, CopyButton, cx, Empty, ErrorNotice, ListSkeleton, PageHeader, Segmented } from "../components/ui";

type Mode = "layers" | "final";

// The page is a PC workbench: two tab rows (prompt layers vs. full prompts)
// with one editor at a time. Every layer keeps its own tab so adjusting one
// never means scrolling past the other six.
export default function PromptsView() {
  const { platforms, notify, focusTarget } = useApp();
  const layers = useAsync(() => api<PromptLayerEdit[]>("/api/v1/prompts/layers"), []);
  const prompts = useAsync(() => api<PromptView[]>("/api/v1/prompts"), [platforms]);
  const [mode, setMode] = useState<Mode>("layers");
  const [activeLayer, setActiveLayer] = useState("");
  const [activeCall, setActiveCall] = useState("");

  const layerList = Array.isArray(layers.data) ? layers.data : [];
  const list = Array.isArray(prompts.data) ? prompts.data : [];

  const refresh = () => {
    layers.reload();
    prompts.reload();
  };

  // Keep the open tab pointing at something real after a reload, a reset or a
  // first load.
  useEffect(() => {
    if (layerList.length && !layerList.some((layer) => layer.key === activeLayer)) {
      setActiveLayer(layerList[0].key);
    }
  }, [layers.data, activeLayer]);

  useEffect(() => {
    if (list.length && !list.some((item) => item.call === activeCall)) {
      setActiveCall(list[0].call);
    }
  }, [prompts.data, activeCall]);

  // `prompt:<key>` is the deep link another screen (or the full-prompt layer
  // list) uses to open one text layer's editor.
  useEffect(() => {
    if (!focusTarget.startsWith("prompt:")) return;
    setMode("layers");
    setActiveLayer(focusTarget.slice("prompt:".length));
  }, [focusTarget]);

  const editLayer = (key: string) => {
    setMode("layers");
    setActiveLayer(key);
  };

  const selectedLayer = layerList.find((layer) => layer.key === activeLayer);
  const selectedPrompt = list.find((item) => item.call === activeCall);

  return (
    <>
      <PageHeader
        title={t("prompts.title")}
        description={t("prompts.description")}
        actions={
          <Button variant="secondary" icon={RefreshCw} loading={prompts.loading && !!prompts.data} onClick={refresh}>
            {t("common.refresh")}
          </Button>
        }
      />
      {layers.error ? <ErrorNotice error={layers.error} className="mb-4" /> : null}
      {prompts.error ? <ErrorNotice error={prompts.error} className="mb-4" /> : null}

      <Card>
        <div className="flex flex-wrap items-center gap-2 border-b border-zinc-100 px-4 py-2.5 dark:border-zinc-800">
          <Segmented
            value={mode}
            onChange={setMode}
            options={[
              { value: "layers", label: t("prompts.tabLayers") },
              { value: "final", label: t("prompts.tabFinal") },
            ]}
          />
        </div>

        <div
          role="tablist"
          aria-label={t("prompts.tabsLabel")}
          className="flex gap-1 overflow-x-auto border-b border-zinc-100 px-3 py-2 dark:border-zinc-800"
        >
          {mode === "layers"
            ? layerList.map((layer) => (
                <TabButton key={layer.key} active={layer.key === activeLayer} onClick={() => setActiveLayer(layer.key)}>
                  {layer.name}
                  {layer.customized ? <span className="size-1.5 rounded-full bg-emerald-500" title={t("prompts.customized")} /> : null}
                </TabButton>
              ))
            : list.map((item) => (
                <TabButton key={item.call} active={item.call === activeCall} onClick={() => setActiveCall(item.call)}>
                  {item.title}
                </TabButton>
              ))}
        </div>

        <div className="p-4">
          {mode === "layers" ? <p className="mb-2 text-xs text-zinc-500 dark:text-zinc-400">{t("prompts.layersDesc")}</p> : null}
          {mode === "layers" ? (
            layers.loading && !layers.data ? (
              <ListSkeleton rows={3} />
            ) : selectedLayer ? (
              <LayerEditor
                key={selectedLayer.key}
                layer={selectedLayer}
                onSaved={() => {
                  notify({ kind: "success", title: t("prompts.saved") });
                  refresh();
                }}
                onReset={() => {
                  notify({ kind: "success", title: t("prompts.resetDone") });
                  refresh();
                }}
                onConflict={refresh}
              />
            ) : (
              <Empty icon={ScrollText} title={t("prompts.empty")} />
            )
          ) : prompts.loading && !prompts.data ? (
            <ListSkeleton rows={3} />
          ) : selectedPrompt ? (
            <PromptDetail prompt={selectedPrompt} onEditLayer={editLayer} />
          ) : (
            <Empty icon={ScrollText} title={t("prompts.empty")} />
          )}
        </div>
      </Card>
    </>
  );
}

function TabButton({ active, onClick, children }: { active: boolean; onClick: () => void; children: ReactNode }) {
  return (
    <button
      type="button"
      role="tab"
      aria-selected={active}
      onClick={onClick}
      className={cx(
        "inline-flex shrink-0 items-center gap-1.5 rounded-md px-3 py-1.5 text-[13px] font-medium transition",
        active
          ? "bg-zinc-900 text-white dark:bg-zinc-100 dark:text-zinc-900"
          : "text-zinc-600 hover:bg-zinc-100 dark:text-zinc-300 dark:hover:bg-zinc-800",
      )}
    >
      {children}
    </button>
  );
}

function LayerEditor({
  layer,
  onSaved,
  onReset,
  onConflict,
}: {
  layer: PromptLayerEdit;
  onSaved: () => void;
  onReset: () => void;
  onConflict: () => void;
}) {
  const { notify } = useApp();
  const [text, setText] = useState(layer.text);
  const [busy, setBusy] = useState<"save" | "reset" | null>(null);
  const baseRef = useRef(layer.text);
  const forceRef = useRef(false);

  // A save or reset reloads the list; the effect keeps the textarea in step
  // with whatever the server now holds, unless there are unsaved edits (a
  // conflict refresh must not throw away typing).
  useEffect(() => {
    const previousBase = baseRef.current;
    const force = forceRef.current;
    baseRef.current = layer.text;
    forceRef.current = false;
    setText((current) => (force || current === previousBase ? layer.text : current));
  }, [layer.text, layer.revision]);

  const dirty = text !== layer.text;
  const canSave = busy === null && text.trim() !== "" && dirty;

  const fail = (reason: unknown, title: string) => {
    if (reason instanceof ApiError && reason.code === "REVISION_CONFLICT") {
      onConflict();
      notify({ kind: "error", title, detail: t("common.conflictReloaded") });
      return;
    }
    toastError(notify, reason, title);
  };

  const save = async () => {
    if (!canSave) return;
    setBusy("save");
    try {
      await putJSON<PromptLayerEdit>(`/api/v1/prompts/layers/${layer.key}`, {
        text,
        if_revision: layer.revision,
      });
      onSaved();
    } catch (reason) {
      fail(reason, t("common.saveFailed"));
    } finally {
      setBusy(null);
    }
  };

  const reset = async () => {
    setBusy("reset");
    try {
      await postJSON<PromptLayerEdit>(`/api/v1/prompts/layers/${layer.key}/reset`, {
        if_revision: layer.revision,
      });
      forceRef.current = true;
      onReset();
    } catch (reason) {
      fail(reason, t("styles.resetFailed"));
    } finally {
      setBusy(null);
    }
  };

  return (
    <div className="space-y-2">
      <div className="flex flex-wrap items-center gap-2">
        <span className="text-sm font-medium">{layer.name}</span>
        {layer.customized ? <Badge tone="green">{t("prompts.customized")}</Badge> : <Badge tone="neutral">{t("prompts.builtin")}</Badge>}
        <span className="font-mono text-[11px] text-zinc-400">
          {layer.key}
          {layer.revision > 0 ? ` · r${layer.revision}` : ""}
        </span>
        <span className="text-xs text-zinc-500">{t("prompts.chars", { count: [...text].length })}</span>
        <div className="ml-auto flex items-center gap-2">
          {layer.customized ? (
            <Button variant="subtle" size="sm" icon={RotateCcw} loading={busy === "reset"} disabled={busy !== null} onClick={reset}>
              {t("prompts.reset")}
            </Button>
          ) : null}
          <Button size="sm" loading={busy === "save"} disabled={!canSave} onClick={save}>
            {t("prompts.save")}
          </Button>
        </div>
      </div>
      <textarea
        className="field-input min-h-[26rem] w-full resize-y font-mono text-xs leading-relaxed"
        rows={18}
        spellCheck={false}
        value={text}
        aria-label={layer.name}
        onChange={(event) => setText(event.target.value)}
        onKeyDown={(event) => {
          if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "s") {
            event.preventDefault();
            void save();
          }
        }}
      />
      <p className="text-xs text-zinc-500 dark:text-zinc-400">{t("prompts.saveHint")}</p>
    </div>
  );
}

function PromptDetail({ prompt, onEditLayer }: { prompt: PromptView; onEditLayer: (key: string) => void }) {
  const layers = Array.isArray(prompt.layers) ? prompt.layers : [];
  const editable = layers.filter((layer) => layer.editable).length;
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1.5">
        <Badge tone="neutral" className="font-mono">
          {prompt.call}
        </Badge>
        <span className="text-xs text-zinc-500">
          {t("prompts.summary", { layers: layers.length, editable, chars: [...(prompt.text ?? "")].length })}
        </span>
        <span className="max-w-full truncate font-mono text-[11px] text-zinc-400" title={`${prompt.version} · sha256 ${prompt.sha256}`}>
          {prompt.version}
        </span>
        <span className="ml-auto">
          <CopyButton text={prompt.text ?? ""} label={t("common.copyAll")} />
        </span>
      </div>

      <div className="grid items-start gap-3 xl:grid-cols-[minmax(0,1fr)_minmax(0,1.15fr)]">
        <div>
          <p className="mb-1.5 text-xs font-medium text-zinc-500 dark:text-zinc-400">{t("prompts.layerBreakdown")}</p>
          <ol className="max-h-[36rem] space-y-2 overflow-y-auto pr-1">
            {layers.map((layer, index) => (
              <LayerBlock key={`${layer.name}-${index}`} layer={layer} index={index} onEditLayer={onEditLayer} />
            ))}
          </ol>
        </div>
        <div>
          <div className="mb-1.5 flex items-center gap-2">
            <p className="text-xs font-medium text-zinc-500 dark:text-zinc-400">{t("prompts.final")}</p>
            <span className="truncate font-mono text-[11px] text-zinc-400" title={`sha256 ${prompt.sha256}`}>
              sha256 {prompt.sha256.slice(0, 16)}…
            </span>
          </div>
          <pre className="code-block max-h-[36rem] overflow-auto">
            {prompt.text}
          </pre>
        </div>
      </div>
    </div>
  );
}

function LayerBlock({ layer, index, onEditLayer }: { layer: PromptLayer; index: number; onEditLayer: (key: string) => void }) {
  const { jumpTo } = useApp();
  const target = layer.edit_target ?? "";
  // "platform:gongzhonghao" -> that platform's 配置 screen; "prompt:extraction"
  // -> the layer editor on this screen. The link is only drawn when the target
  // actually maps to somewhere, so a layer never advertises a dead edit path.
  const configView = target.startsWith("platform:") ? configViewFor(target.slice("platform:".length)) : undefined;
  const promptKey = target.startsWith("prompt:") ? target.slice("prompt:".length) : "";
  const editableTarget = configView || promptKey;
  const go = () => {
    if (configView) {
      jumpTo(configView);
    } else if (promptKey) {
      onEditLayer(promptKey);
    }
  };
  return (
    <li className={cx("rounded-lg border p-2.5", layer.editable ? "border-emerald-200 dark:border-emerald-500/30" : "border-zinc-200 dark:border-zinc-800")}>
      <div className="flex flex-wrap items-center gap-2">
        <span className="grid size-5 place-items-center rounded-full bg-zinc-100 text-[11px] font-semibold tabular-nums text-zinc-600 dark:bg-zinc-800 dark:text-zinc-300">{index + 1}</span>
        <span className="text-sm font-medium">{layer.name}</span>
        {layer.editable ? (
          <Badge tone="green">{t("prompts.editable")}</Badge>
        ) : (
          <Badge tone="neutral">
            <Lock className="size-3" />
            {t("prompts.readonly")}
          </Badge>
        )}
        <span className="text-xs text-zinc-500">{layer.source}</span>
        {editableTarget ? (
          <Button variant="subtle" size="sm" icon={PencilLine} className="ml-auto" onClick={go}>
            {promptKey ? t("prompts.edit") : t("prompts.goConfig")}
          </Button>
        ) : null}
      </div>
      <pre className="code-block mt-2 max-h-40 overflow-y-auto text-[11px]">
        {layer.text || t("prompts.emptyLayer")}
      </pre>
    </li>
  );
}
