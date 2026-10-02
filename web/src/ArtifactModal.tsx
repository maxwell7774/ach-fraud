import { createEffect, createSignal, onCleanup, Show } from "solid-js";
import { FileViewer, Loading } from "./components";

export interface ArtifactRef {
  id: string;
  kind: string;
  filename: string;
  checksum: string;
  state: string;
}

export default function ArtifactFileModal(props: {
  artifact: ArtifactRef | null | undefined;
  content: string;
  loading: boolean;
  error: string;
  onClose: () => void;
}) {
  let dlg: HTMLDialogElement | undefined;
  const [copied, setCopied] = createSignal(false);
  let copyTimer: number | undefined;

  createEffect(() => {
    const a = props.artifact;
    if (a) {
      setCopied(false);
      queueMicrotask(() => {
        if (dlg && !dlg.open) dlg.showModal();
      });
    } else {
      if (dlg?.open) dlg.close();
    }
  });

  onCleanup(() => {
    if (copyTimer !== undefined) window.clearTimeout(copyTimer);
    if (dlg?.open) dlg.close();
  });

  function close() {
    props.onClose();
  }

  async function copyContent() {
    try {
      await navigator.clipboard.writeText(props.content);
    } catch {
      const ta = document.createElement("textarea");
      ta.value = props.content;
      document.body.appendChild(ta);
      ta.select();
      document.execCommand("copy");
      ta.remove();
    }
    setCopied(true);
    if (copyTimer !== undefined) window.clearTimeout(copyTimer);
    copyTimer = window.setTimeout(() => setCopied(false), 1500);
  }

  function downloadContent() {
    const a = props.artifact;
    if (!a) return;
    const blob = new Blob([props.content], { type: "text/plain" });
    const url = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = url;
    link.download = `${a.filename}-${a.kind}.ach`;
    document.body.appendChild(link);
    link.click();
    link.remove();
    URL.revokeObjectURL(url);
  }

  const canAct = () => !props.loading && !props.error && props.content.length > 0;

  return (
    <dialog
      id="artifactDlg"
      ref={dlg}
      aria-label={props.artifact ? `${props.artifact.kind} file ${props.artifact.filename}` : "ACH file"}
      onCancel={(e) => {
        e.preventDefault();
        close();
      }}
      onClick={(e) => {
        if (e.target === dlg) close();
      }}
    >
      <Show when={props.artifact}>
        {(a) => (
          <>
            <div class="artifact-head">
              <div class="artifact-title">
                {a().kind} <span class="muted">—</span> {a().filename}
              </div>
              <div class="artifact-sub muted">
                {a().checksum} · {a().state}
              </div>
            </div>
            <div class="artifact-body">
              <Show
                when={!props.loading}
                fallback={<Loading label="Loading artifact…" />}
              >
                <Show
                  when={!props.error}
                  fallback={<p class="error issue">{props.error}</p>}
                >
                  <FileViewer content={props.content} />
                </Show>
              </Show>
            </div>
            <div class="dialog-actions">
              <button
                class="btn btn-approve btn-sm"
                type="button"
                disabled={!canAct()}
                onClick={copyContent}
              >
                {copied() ? "Copied!" : "Copy"}
              </button>
              <button
                class="btn btn-approve btn-sm"
                type="button"
                disabled={!canAct()}
                onClick={downloadContent}
              >
                Download
              </button>
              <button class="btn btn-outline btn-sm" type="button" onClick={close}>
                Close
              </button>
            </div>
          </>
        )}
      </Show>
    </dialog>
  );
}
