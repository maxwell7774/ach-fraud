import { createContext, createSignal, Show, useContext } from "solid-js";
import type { JSX, ParentProps } from "solid-js";
import { XIcon } from "./icons";

type Flash = { type: "success" | "error"; msg: string };

const FlashCtx = createContext<{ show: (type: "success" | "error", msg: string) => void }>();

export function FlashProvider(props: ParentProps): JSX.Element {
  const [flash, setFlash] = createSignal<Flash | null>(null);
  let timer: ReturnType<typeof setTimeout> | undefined;

  function show(type: "success" | "error", msg: string) {
    if (timer) clearTimeout(timer);
    setFlash({ type, msg });
    timer = setTimeout(() => setFlash(null), 5000);
  }

  return (
    <FlashCtx.Provider value={{ show }}>
      {props.children}
      <Show when={flash()}>
        {(f) => (
          <div class={`toast toast-${f().type}`} role="status" aria-live="polite">
            <span>{f().msg}</span>
            <button class="toast-close" onClick={() => setFlash(null)} aria-label="Dismiss">
              <XIcon />
            </button>
          </div>
        )}
      </Show>
    </FlashCtx.Provider>
  );
}

export function useFlash() {
  const ctx = useContext(FlashCtx);
  if (!ctx) throw new Error("useFlash outside FlashProvider");
  return ctx;
}
