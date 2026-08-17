import { createContext, createSignal, Show, useContext } from "solid-js";
import type { JSX, ParentProps } from "solid-js";
import { AlertIcon } from "./icons";

export type ConfirmTone = "approve" | "decline";

export type ConfirmOptions = {
  title?: string;
  tone?: ConfirmTone;
  confirmLabel?: string;
  showNote?: boolean;
};

export type ConfirmResult = { ok: boolean; note: string };

type ConfirmState = { msg: string; opts: ConfirmOptions; resolve: (r: ConfirmResult) => void };

const ConfirmCtx = createContext<{ confirm: (msg: string, opts?: ConfirmOptions) => Promise<ConfirmResult> }>();

export function ConfirmProvider(props: ParentProps): JSX.Element {
  const [state, setState] = createSignal<ConfirmState | null>(null);
  const [note, setNote] = createSignal("");
  let dlg: HTMLDialogElement | undefined;

  function confirm(msg: string, opts: ConfirmOptions = {}): Promise<ConfirmResult> {
    return new Promise((resolve) => {
      setNote("");
      setState({ msg, opts, resolve });
      queueMicrotask(() => dlg?.showModal());
    });
  }
  function done(ok: boolean) {
    state()?.resolve({ ok, note: note() });
    setState(null);
    dlg?.close();
  }

  return (
    <ConfirmCtx.Provider value={{ confirm }}>
      <Show when={state()}>
        {(s) => (
          <dialog
            id="confirmDlg"
            ref={dlg}
            onCancel={(e) => {
              e.preventDefault();
              done(false);
            }}
            class={s().opts.tone === "decline" ? "confirm-decline" : "confirm-approve"}
          >
            <div class="confirm-head">
              <span class="confirm-icon">
                <AlertIcon />
              </span>
              <span class="confirm-title">{s().opts.title ?? "Please confirm"}</span>
            </div>
            <p>{s().msg}</p>
            <Show when={s().opts.showNote}>
              <div class="confirm-note">
                <label>
                  Note <span class="muted">(optional)</span>
                  <input
                    type="text"
                    placeholder="Add a note…"
                    value={note()}
                    onInput={(e) => setNote(e.currentTarget.value)}
                  />
                </label>
              </div>
            </Show>
            <div class="dialog-actions">
              <button class="btn btn-outline" onClick={() => done(false)}>
                Cancel
              </button>
              <button
                class={`btn ${s().opts.tone === "decline" ? "btn-decline" : "btn-approve"}`}
                onClick={() => done(true)}
              >
                {s().opts.confirmLabel ?? "Confirm"}
              </button>
            </div>
          </dialog>
        )}
      </Show>
      {props.children}
    </ConfirmCtx.Provider>
  );
}

export function useConfirm() {
  const ctx = useContext(ConfirmCtx);
  if (!ctx) throw new Error("useConfirm outside ConfirmProvider");
  return ctx;
}
