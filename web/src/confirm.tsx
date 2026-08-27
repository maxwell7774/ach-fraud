import { createContext, createSignal, Show, For, useContext } from "solid-js";
import type { JSX, ParentProps } from "solid-js";
import { AlertIcon } from "./icons";

export type ConfirmTone = "approve" | "decline" | "neutral";

export type ConfirmOptions = {
  title?: string;
  tone?: ConfirmTone;
  confirmLabel?: string;
  showNote?: boolean;
  // choices renders a single-select radio group when present; the selected
  // value is returned as `choice` (defaults to the first option).
  choices?: { value: string; label: string }[];
};

export type ConfirmResult = { ok: boolean; note: string; choice?: string };

type ConfirmState = { msg: string; opts: ConfirmOptions; resolve: (r: ConfirmResult) => void };

const ConfirmCtx = createContext<{ confirm: (msg: string, opts?: ConfirmOptions) => Promise<ConfirmResult> }>();

export function ConfirmProvider(props: ParentProps): JSX.Element {
  const [state, setState] = createSignal<ConfirmState | null>(null);
  const [note, setNote] = createSignal("");
  const [choice, setChoice] = createSignal("");
  let dlg: HTMLDialogElement | undefined;

  function confirm(msg: string, opts: ConfirmOptions = {}): Promise<ConfirmResult> {
    return new Promise((resolve) => {
      setNote("");
      setChoice(opts.choices?.[0]?.value ?? "");
      setState({ msg, opts, resolve });
      queueMicrotask(() => dlg?.showModal());
    });
  }
  function done(ok: boolean) {
    state()?.resolve({ ok, note: note(), choice: choice() });
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
            class={s().opts.tone === "decline" ? "confirm-decline" : s().opts.tone === "approve" ? "confirm-approve" : "confirm-neutral"}
          >
            <div class="confirm-head">
              <span class="confirm-icon">
                <AlertIcon />
              </span>
              <span class="confirm-title">{s().opts.title ?? "Please confirm"}</span>
            </div>
            <p>{s().msg}</p>
            <Show when={s().opts.choices}>
              <div class="confirm-choices">
                <For each={s().opts.choices!}>
                  {(c) => (
                    <label class={`choice ${choice() === c.value ? "selected" : ""}`}>
                      <input
                        type="radio"
                        name="confirm-choice"
                        value={c.value}
                        checked={choice() === c.value}
                        onChange={() => setChoice(c.value)}
                      />
                      {c.label}
                    </label>
                  )}
                </For>
              </div>
            </Show>
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
                class={`btn ${s().opts.tone === "decline" ? "btn-decline" : s().opts.tone === "approve" ? "btn-approve" : ""}`}
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
