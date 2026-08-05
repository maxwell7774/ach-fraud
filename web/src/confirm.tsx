import { createContext, createSignal, Show, useContext } from "solid-js";
import type { JSX, ParentProps } from "solid-js";
import { AlertIcon } from "./icons";

export type ConfirmTone = "approve" | "decline";

export type ConfirmOptions = {
  title?: string;
  tone?: ConfirmTone;
};

type ConfirmState = { msg: string; opts: ConfirmOptions; resolve: (ok: boolean) => void };

const ConfirmCtx = createContext<{ confirm: (msg: string, opts?: ConfirmOptions) => Promise<boolean> }>();

export function ConfirmProvider(props: ParentProps): JSX.Element {
  const [state, setState] = createSignal<ConfirmState | null>(null);
  let dlg: HTMLDialogElement | undefined;

  function confirm(msg: string, opts: ConfirmOptions = {}): Promise<boolean> {
    return new Promise((resolve) => {
      setState({ msg, opts, resolve });
      // showModal after the freshly-mounted dialog exists.
      queueMicrotask(() => dlg?.showModal());
    });
  }
  function done(ok: boolean) {
    state()?.resolve(ok);
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
            class={s().opts.tone === "decline" ? "confirm-decline" : "confirm-approve"}
          >
            <div class="confirm-head">
              <span class="confirm-icon">
                <AlertIcon />
              </span>
              <span class="confirm-title">{s().opts.title ?? "Please confirm"}</span>
            </div>
            <p>{s().msg}</p>
            <div class="dialog-actions">
              <button class="btn btn-outline" onClick={() => done(false)}>
                Cancel
              </button>
              <button
                class={`btn ${s().opts.tone === "decline" ? "btn-decline" : "btn-approve"}`}
                onClick={() => done(true)}
              >
                Confirm
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
