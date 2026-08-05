import { createSignal, For, Show } from "solid-js";
import { Router, Route, A } from "@solidjs/router";
import { FlashProvider } from "./flash";
import { ConfirmProvider } from "./confirm";
import ThemeToggle from "./ThemeToggle";
import Dashboard from "./pages/Dashboard";
import Holds from "./pages/Holds";
import HoldDetail from "./pages/HoldDetail";
import Submissions from "./pages/Submissions";
import SubmissionDetail from "./pages/SubmissionDetail";
import Entries from "./pages/Entries";
import Headers from "./pages/Headers";
import Events from "./pages/Events";
import { MenuIcon, XIcon } from "./icons";

function Brand() {
  return (
    <span class="brand-mark" aria-hidden="true">
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
        <path d="M12 2 4 5.5v6c0 4.6 3.2 8.4 8 9.5 4.8-1.1 8-4.9 8-9.5v-6L12 2z" />
        <polyline points="8.5 11.5 11 14 15.5 9" />
      </svg>
    </span>
  );
}

const LINKS = [
  { href: "/", label: "Dashboard", end: true },
  { href: "/holds", label: "Holds" },
  { href: "/submissions", label: "Files" },
  { href: "/entries", label: "Entries" },
  { href: "/headers", label: "Headers" },
  { href: "/events", label: "Events" },
];

function Layout(props: { children?: any }) {
  const [menuOpen, setMenuOpen] = createSignal(false);
  const close = () => setMenuOpen(false);
  return (
    <>
      <nav>
        <div class="nav-inner">
          <A class="brand" href="/" onClick={close}>
            <Brand />
            ACH FRAUD
          </A>
          <div class="nav-links">
            <For each={LINKS}>
              {(l) => (
                <A href={l.href} end={l.end}>
                  {l.label}
                </A>
              )}
            </For>
          </div>
          <div class="spacer" />
          <ThemeToggle />
          <button
            class="nav-burger"
            type="button"
            aria-label={menuOpen() ? "Close menu" : "Open menu"}
            aria-expanded={menuOpen()}
            onClick={() => setMenuOpen((o) => !o)}
          >
            {menuOpen() ? <XIcon /> : <MenuIcon />}
          </button>
        </div>
        <Show when={menuOpen()}>
          <div class="nav-menu">
            <For each={LINKS}>
              {(l) => (
                <A href={l.href} end={l.end} onClick={close}>
                  {l.label}
                </A>
              )}
            </For>
          </div>
        </Show>
      </nav>
      <main class="container" onClick={close}>
        <FlashProvider>
          <ConfirmProvider>{props.children}</ConfirmProvider>
        </FlashProvider>
      </main>
    </>
  );
}

export default function App() {
  return (
    <Router root={Layout}>
      <Route path="/" component={Dashboard} />
      <Route path="/holds" component={Holds} />
      <Route path="/holds/:id" component={HoldDetail} />
      <Route path="/submissions" component={Submissions} />
      <Route path="/submissions/:id" component={SubmissionDetail} />
      <Route path="/entries" component={Entries} />
      <Route path="/headers" component={Headers} />
      <Route path="/events" component={Events} />
      <Route path="*404" component={() => <p>Not found</p>} />
    </Router>
  );
}
