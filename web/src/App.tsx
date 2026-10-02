import { createEffect, createSignal, For, Show, ErrorBoundary } from "solid-js";
import { Router, Route, A, useAction } from "@solidjs/router";
import { FlashProvider } from "./flash";
import { ConfirmProvider } from "./confirm";
import ThemeToggle from "./ThemeToggle";
import Dashboard from "./pages/Dashboard";
import Holds from "./pages/Holds";
import HoldDetail from "./pages/HoldDetail";
import Submissions from "./pages/Submissions";
import SubmissionDetail from "./pages/SubmissionDetail";
import Entries from "./pages/Entries";
import EntryDetail from "./pages/EntryDetail";
import Events from "./pages/Events";
import Jobs from "./pages/Jobs";
import Recipients from "./pages/Recipients";
import { Loading } from "./components";
import { MenuIcon, XIcon } from "./icons";
import { setAuthDisabled, setCurrentUser, currentUser, isSuperAdmin } from "./user";
import { api } from "./api";
import { logoutAction } from "./queries";

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
  { href: "/events", label: "Events", superAdmin: true },
  { href: "/jobs", label: "Jobs", superAdmin: true },
  { href: "/recipients", label: "Recipients", superAdmin: true },
];

function Layout(props: { children?: any }) {
  const [menuOpen, setMenuOpen] = createSignal(false);
  const close = () => setMenuOpen(false);
  const visibleLinks = () => LINKS.filter((l) => !l.superAdmin || isSuperAdmin());
  const user = currentUser;
  const roleLabel = (r?: string) =>
    r === "super_admin"
      ? "Super admin"
      : r === "admin"
        ? "Admin"
        : r === "processor"
          ? "Processor"
          : r === "watcher"
            ? "Watcher"
            : "";
  const logout = useAction(logoutAction);
  async function signOut() {
    try {
      await logout();
    } catch {
      /* session already gone */
    }
    window.location.assign("/");
  }
  return (
    <>
      <nav>
        <div class="nav-inner">
          <A class="brand" href="/" onClick={close}>
            <Brand />
            ACH FRAUD
          </A>
          <div class="nav-links">
            <For each={visibleLinks()}>
              {(l) => (
                <A href={l.href} end={l.end}>
                  {l.label}
                </A>
              )}
            </For>
          </div>
          <div class="spacer" />
          <Show when={user()}>
            <div class="nav-user">
              <span class="nav-user-name" title={user()!.upn}>
                {user()!.name || user()!.upn}
                <Show when={roleLabel(user()!.role)}>
                  <span class="nav-user-role"> · {roleLabel(user()!.role)}</span>
                </Show>
              </span>
              <button class="nav-user-logout" type="button" onClick={signOut}>
                Sign out
              </button>
            </div>
          </Show>
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
            <For each={visibleLinks()}>
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

function LoginScreen() {
  return (
    <div class="login-page">
      <div class="login-card">
        <span class="login-brand" aria-hidden="true">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <path d="M12 2 4 5.5v6c0 4.6 3.2 8.4 8 9.5 4.8-1.1 8-4.9 8-9.5v-6L12 2z" />
            <polyline points="8.5 11.5 11 14 15.5 9" />
          </svg>
        </span>
        <h1>ACH Fraud Review</h1>
        <p>Sign in to review holds and manage releases.</p>
        <a class="btn btn-approve login-btn" href="/api/auth/login">
          <svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
            <path d="M3 3h8v8H3zM13 3h8v8h-8zM3 13h8v8H3zM13 13h8v8h-8z" />
          </svg>
          Sign in with Microsoft
        </a>
      </div>
    </div>
  );
}

type GateState = "loading" | "login" | "app";

export default function App() {
  const [state, setState] = createSignal<GateState>("loading");

  createEffect(() => {
    api
      .me()
      .then((u) => {
        setCurrentUser(u);
        setAuthDisabled(false);
        setState("app");
      })
      .catch((e) => {
        // 404 means auth isn't enabled (dev/e2e mode); anything else means we
        // need to sign in.
        if (e.status === 404) {
          setAuthDisabled(true);
          setState("app");
        } else {
          setState("login");
        }
      });
  });

  return (
    <Show
      when={state() !== "loading"}
      fallback={
        <div class="container">
          <Loading label="Checking session…" />
        </div>
      }
    >
      <Show
        when={state() === "login"}
        fallback={
          <ErrorBoundary fallback={(err, reset) => (
            <div class="container">
              <p class="empty">Something went wrong.</p>
              <div class="empty-actions">
                <button class="btn btn-outline btn-sm" onClick={reset}>Retry</button>
              </div>
            </div>
          )}>
          <Router root={Layout}>
            <Route path="/" component={Dashboard} />
            <Route path="/holds" component={Holds} />
            <Route path="/holds/:id" component={HoldDetail} />
            <Route path="/submissions" component={Submissions} />
            <Route path="/submissions/:id" component={SubmissionDetail} />
            <Route path="/entries" component={Entries} />
            <Route path="/entries/:id" component={EntryDetail} />
            <Route path="/events" component={Events} />
            <Route path="/jobs" component={Jobs} />
            <Route path="/recipients" component={Recipients} />
            <Route path="*404" component={() => <p>Not found</p>} />
          </Router>
          </ErrorBoundary>
        }
      >
        <LoginScreen />
      </Show>
    </Show>
  );
}
