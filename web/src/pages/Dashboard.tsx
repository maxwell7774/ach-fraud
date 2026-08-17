import { createSignal, For, Show } from "solid-js";
import { A } from "@solidjs/router";
import { createAsync, revalidate } from "@solidjs/router";
import { dashboardQuery, usePolling } from "../queries";
import { api, dollars, fmtDate } from "../api";
import { Cards, HoldTable, Loading, PageHeader, QueryError } from "../components";

const firstLine = (s: string) => s.split("\n")[0];

export default function Dashboard() {
  const { remaining } = usePolling("dashboard");
  const [error, setError] = createSignal<unknown>();
  const data = createAsync(() => { setError(undefined); return dashboardQuery().catch((e) => { setError(e); return undefined; }); });
  return (
    <Show when={data()} fallback={<Show when={error()} fallback={<Loading label="Loading dashboard…" />}><QueryError error={error()} onRetry={() => revalidate("dashboard")} /></Show>}>
      {(d) => (
        <>
          <PageHeader remaining={remaining}>
            <h1>Dashboard</h1>
          </PageHeader>
          <Cards counts={d().hold_counts} />

          <Show when={d().failed_submissions.length > 0 || d().failed_jobs.length > 0}>
            <div class="alert">
              <strong>Needs attention</strong>
              <Show when={d().failed_submissions.length > 0}>
                <ul>
                  <For each={d().failed_submissions}>
                    {(s) => (
                      <li>
                        <A href={`/submissions/${s.id}`} class="file-link">
                          <strong>{s.filename}</strong>
                        </A>{" "}
                        — failed to import: {firstLine(s.failed_reason)}
                      </li>
                    )}
                  </For>
                </ul>
              </Show>
              <Show when={d().failed_jobs.length > 0}>
                <ul>
                  <For each={d().failed_jobs}>
                    {(j) => (
                      <li>
                        <Show
                          when={j.submission_id}
                          fallback={
                            <strong>
                              {j.filename} · {j.kind.replace(/_/g, " ")}
                            </strong>
                          }
                        >
                          <A href={`/submissions/${j.submission_id}`} class="file-link">
                            <strong>{j.filename}</strong>
                          </A>
                        </Show>{" "}
                        — {j.kind.replace(/_/g, " ")} job failed: {firstLine(j.last_error)}
                      </li>
                    )}
                  </For>
                </ul>
              </Show>
            </div>
          </Show>

          <Show when={d().velocity_leaks.length > 0}>
            <h2 class="section">Velocity leaks</h2>
            <p class="section-note">
              Funds that shipped earlier in the same day before a velocity split was caught.
            </p>
            <div class="table-wrap">
              <table class="responsive">
                <thead>
                  <tr>
                    <th>Account</th>
                    <th>File</th>
                    <th>Effective</th>
                    <th>Same-day total</th>
                    <th>Already shipped</th>
                    <th>Held</th>
                  </tr>
                </thead>
                <tbody>
                  <For each={d().velocity_leaks}>
                    {(l) => (
                      <tr>
                        <td data-label="Account">{l.account}</td>
                        <td data-label="File" class="muted">
                          {l.filename}
                        </td>
                        <td data-label="Effective">{fmtDate(l.effective_date)}</td>
                        <td data-label="Same-day total" class="amount">
                          {dollars(l.total)}
                        </td>
                        <td data-label="Already shipped" class="amount leak">
                          {dollars(l.prior)}
                        </td>
                        <td data-label="Held" class="amount">
                          {dollars(l.held)}
                        </td>
                      </tr>
                    )}
                  </For>
                </tbody>
              </table>
            </div>
          </Show>

          <h2 class="section">
            Pending review{" "}
            <A href="/holds" class="section-link">
              view all
            </A>
          </h2>
          <Show
            when={d().pending.length > 0}
            fallback={<p class="empty">No pending holds. Good to go.</p>}
          >
            <HoldTable holds={d().pending} />
          </Show>
        </>
      )}
    </Show>
  );
}
