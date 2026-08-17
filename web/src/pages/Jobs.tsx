import { createMemo, createSignal, For, Show } from "solid-js";
import { createAsync, useAction, revalidate } from "@solidjs/router";
import { jobsQuery, requeueJobAction, usePolling } from "../queries";
import { api, fmtDateTime, human, type Job } from "../api";
import { EmptyState, Loading, Select, PageHeader, QueryError } from "../components";
import { useFlash } from "../flash";
import { useConfirm } from "../confirm";

const STATES = [
  { value: "failed", label: "Failed" },
  { value: "queued", label: "Queued" },
  { value: "in_progress", label: "In progress" },
  { value: "done", label: "Done" },
];

export default function Jobs() {
  const { remaining } = usePolling("jobs");
  const { show: flash } = useFlash();
  const { confirm } = useConfirm();
  const requeue = useAction(requeueJobAction);
  const [state, setState] = createSignal("failed");
  const [queryError, setQueryError] = createSignal<unknown>();

  const key = createMemo(() => ({ state: state() }));
  const data = createAsync(() => { setQueryError(undefined); return jobsQuery(key()).catch((e) => { setQueryError(e); return undefined; }); });

  async function onRequeue(j: Job) {
    const res = await confirm(`Requeue the ${j.kind.replace(/_/g, " ")} job for retry?`, { title: "Requeue job" });
    if (!res.ok) return;
    const result = await requeue({ id: j.id });
    if (!result.ok) {
      flash("error", result.error ?? "failed to requeue job");
      return;
    }
    flash("success", "Job requeued — the next run will retry it");
  }

  return (
    <>
      <PageHeader remaining={remaining}>
        <h1>Jobs</h1>
      </PageHeader>
      <p class="section-note">
        The pipeline outbox. Failed jobs are retried after a code fix or a transient
        error by requeueing them — the next run picks them up again.
      </p>
      <div class="search-bar">
        <label>
          State{" "}
          <Select
            value={state()}
            onChange={setState}
            options={STATES}
          />
        </label>
      </div>
      <Show when={data()} fallback={<Show when={queryError()} fallback={<Loading label="Loading jobs…" />}><QueryError error={queryError()} onRetry={() => revalidate("jobs")} /></Show>}>
        <div class="table-wrap">
          <Show
            when={data()!.jobs.length > 0}
            fallback={<EmptyState message={`No ${state()} jobs.`} />}
          >
            <table class="responsive">
              <thead>
                <tr>
                  <th>Kind</th>
                  <th>State</th>
                  <th>Failures</th>
                  <th>Run at</th>
                  <th>Last error</th>
                  <th>Actions</th>
                </tr>
              </thead>
              <tbody>
                <For each={data()!.jobs}>
                  {(j) => (
                    <tr>
                      <td data-label="Kind">{j.kind.replace(/_/g, " ")}</td>
                      <td data-label="State">{human(j.state)}</td>
                      <td data-label="Failures">{j.failures}</td>
                      <td data-label="Run at" class="muted">
                        {fmtDateTime(j.run_at)}
                      </td>
                      <td data-label="Last error" class="muted">
                        {j.last_error || "—"}
                      </td>
                      <td data-label="Actions">
                        <Show when={j.state === "failed" || j.state === "in_progress"}>
                          <button class="btn btn-outline btn-sm" type="button" onClick={() => onRequeue(j)}>
                            Requeue
                          </button>
                        </Show>
                      </td>
                    </tr>
                  )}
                </For>
              </tbody>
            </table>
          </Show>
        </div>
      </Show>
    </>
  );
}
