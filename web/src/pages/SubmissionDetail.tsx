import { createMemo, createSignal, For, Show } from "solid-js";
import { useParams, A, useAction, createAsync, revalidate } from "@solidjs/router";
import { submissionQuery, verifyQuery, requeueJobAction, usePolling } from "../queries";
import { api, dollars, fmtDateTime, type Job } from "../api";
import { Badge, EmptyState, HoldTable, Loading, Pagination, PageHeader, QueryError } from "../components";
import ArtifactFileModal from "../ArtifactModal";
import { ArrowLeftIcon } from "../icons";
import { useFlash } from "../flash";
import { useConfirm } from "../confirm";
import { canViewRaw, isSuperAdmin } from "../user";

export default function SubmissionDetail() {
  const { remaining } = usePolling("submission", "verify");
  const params = useParams<{ id: string }>();
  const { show: flash } = useFlash();
  const { confirm } = useConfirm();
  const requeue = useAction(requeueJobAction);
  const [holdPage, setHoldPage] = createSignal(1);
  const [holdPageSize, setHoldPageSize] = createSignal(25);
  const [entryPage, setEntryPage] = createSignal(1);
  const [entryPageSize, setEntryPageSize] = createSignal(25);
  const [subError, setSubError] = createSignal<unknown>();
  const [verifyError, setVerifyError] = createSignal<unknown>();
  const sub = createAsync(() => {
    setSubError(undefined);
    return submissionQuery({ id: params.id, page: holdPage(), pageSize: holdPageSize() }).catch((e) => {
      setSubError(e);
      return undefined;
    });
  });
  const verify = createAsync(() => {
    setVerifyError(undefined);
    return verifyQuery({ id: params.id, page: entryPage(), pageSize: entryPageSize() }).catch((e) => {
      setVerifyError(e);
      return undefined;
    });
  });
  const [viewing, setViewing] = createSignal<string | null>(null);
  const [content, setContent] = createSignal("");
  const [artifactError, setArtifactError] = createSignal("");
  const [artifactLoading, setArtifactLoading] = createSignal(false);
  const contentCache = new Map<string, string>();
  const viewingArtifact = createMemo(() => {
    const a = sub()?.artifacts.find((x) => x.id === viewing());
    if (!a) return null;
    return { ...a, filename: sub()?.filename ?? "" };
  });
  // Headline totals: fixed file when processed, else the ingested original.
  const headline = createMemo(() => {
    const arts = sub()?.artifacts ?? [];
    return arts.find((a) => a.kind === "fixed") ?? arts.find((a) => a.kind === "original") ?? null;
  });

  function totalLabel(total?: number, n?: number) {
    return total === undefined ? "—" : `${dollars(total)} (${n ?? 0})`;
  }

  async function viewArtifact(id: string) {
    setViewing(id);
    setArtifactError("");
    const cached = contentCache.get(id);
    if (cached !== undefined) {
      setContent(cached);
      setArtifactLoading(false);
      return;
    }
    setArtifactLoading(true);
    try {
      const text = await api.artifactContent(id);
      contentCache.set(id, text);
      if (viewing() === id) setContent(text);
    } catch (e) {
      if (viewing() === id) {
        setContent("");
        setArtifactError(e instanceof Error ? e.message : "failed to load artifact");
      }
    } finally {
      if (viewing() === id) setArtifactLoading(false);
    }
  }

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
    <Show when={sub()} fallback={<Show when={subError()} fallback={<Loading label="Loading file…" />}><QueryError error={subError()} onRetry={() => revalidate("submission")} /></Show>}>
      {(s) => (
        <>
          <A class="back-link" href="/submissions">
            <ArrowLeftIcon /> Files
          </A>
          <PageHeader remaining={remaining}>
            <h1>
              {s().filename} <Badge status={s().status} />
            </h1>
          </PageHeader>

          <div class="detail-card">
            <h2>Submission</h2>
            <div class="detail-grid">
              <div class="key">Status</div>
              <div class="val">{s().status}</div>
              <div class="key">Failed reason</div>
              <div class="val">{s().failed_reason || "—"}</div>
              <div class="key">Received</div>
              <div class="val">{fmtDateTime(s().received_at)}</div>
              <div class="key">Source checksum</div>
              <div class="val">{s().source_checksum}</div>
              <div class="key">Debit total</div>
              <div class="val">{totalLabel(headline()?.debit_total, headline()?.debit_entries)}</div>
              <div class="key">Credit total</div>
              <div class="val">{totalLabel(headline()?.credit_total, headline()?.credit_entries)}</div>
            </div>
          </div>

          <h2 class="section">Artifacts</h2>
          <div class="table-wrap">
            <table class="responsive">
              <thead>
                <tr>
                  <th>Kind</th>
                  <th>State</th>
                  <th>Checksum</th>
                  <th>Content</th>
                </tr>
              </thead>
              <tbody>
                <For each={s().artifacts}>
                  {(a) => (
                    <tr>
                      <td data-label="Kind">{a.kind}</td>
                      <td data-label="State">
                        <Badge status={a.state} />
                      </td>
                      <td data-label="Checksum" class="muted">{a.checksum}</td>
                      <td data-label="Content">
                        <Show when={canViewRaw()} fallback={<span class="muted">—</span>}>
                          <button class="btn btn-outline btn-sm" onClick={() => viewArtifact(a.id)}>
                            View
                          </button>
                        </Show>
                      </td>
                    </tr>
                  )}
                </For>
              </tbody>
            </table>
          </div>
          <ArtifactFileModal
            artifact={viewingArtifact()}
            content={content()}
            loading={artifactLoading()}
            error={artifactError()}
            onClose={() => setViewing(null)}
          />

          <h2 class="section">Verification</h2>
          <Show when={s().verification} fallback={<p class="empty">No automated verification recorded yet.</p>}>
            {(v) => (
              <div class="detail-card">
                <div class="detail-grid">
                  <div class="key">Last check</div>
                  <div class="val">
                    <span class="verify-line">
                      <Badge status={v().verified ? "verified" : "fail"} />
                      <span>{fmtDateTime(v().checked_at)}</span>
                    </span>
                  </div>
                  <Show when={v().issues}>
                    <div class="key">Issues</div>
                    <div class="val muted">{v().issues}</div>
                  </Show>
                </div>
              </div>
            )}
          </Show>
          <Show when={verify()} fallback={<Show when={verifyError()} fallback={<Loading label="Checking artifact chain…" />}><QueryError error={verifyError()} onRetry={() => revalidate("verify")} /></Show>}>
            {(v) => (
              <>
                <div class="detail-card">
                  <div class="detail-grid">
                    <For each={Object.entries(v().artifacts)}>
                      {([kind, a]) => (
                        <>
                          <div class="key">{kind}</div>
                          <div class="val">
                            <Show
                              when={a.pruned}
                              fallback={
                                a.present ? (
                                  <>
                                    {a.entries} entries · {dollars(a.total)}
                                    <Show when={a.debit_total !== undefined || a.credit_total !== undefined}>
                                      <div class="cell-sub">
                                        debits {totalLabel(a.debit_total, a.debit_entries)} · credits{" "}
                                        {totalLabel(a.credit_total, a.credit_entries)}
                                      </div>
                                    </Show>
                                  </>
                                ) : (
                                  <span class="muted">missing</span>
                                )
                              }
                            >
                              <span class="muted">pruned (retention)</span>
                              <Show when={a.debit_total !== undefined || a.credit_total !== undefined}>
                                <div class="cell-sub">
                                  debits {totalLabel(a.debit_total, a.debit_entries)} · credits{" "}
                                  {totalLabel(a.credit_total, a.credit_entries)}
                                </div>
                              </Show>
                            </Show>
                            <Show when={a.error}>
                              <span class="muted"> ({a.error})</span>
                            </Show>
                          </div>
                        </>
                      )}
                    </For>
                    <div class="key">Chain</div>
                    <div class="val">
                      <Show when={v().pruned} fallback={<Badge status={v().verified ? "verified" : "fail"} />}>
                        <span class="badge badge-pruned">Not verifiable</span>
                      </Show>
                    </div>
                  </div>
                  <Show when={v().issues.length > 0}>
                    <For each={v().issues}>
                      {(issue) => (
                        <Show
                          when={issue.endsWith("pruned; verification unavailable")}
                          fallback={<p class="error issue">• {issue}</p>}
                        >
                          <p class="muted issue">• {issue}</p>
                        </Show>
                      )}
                    </For>
                  </Show>
                </div>

                <Show when={v().entries_total > 0}>
                  <h2 class="section">Entry comparison</h2>
                  <div class="table-wrap">
                    <table class="responsive">
                      <thead>
                        <tr>
                          <th>Trace</th>
                          <th>Sender</th>
                          <th>Amount</th>
                          <th>Original acct</th>
                          <th>Fixed acct</th>
                          <th>Cleaned acct</th>
                          <th>Release acct</th>
                          <th>Receiver kept</th>
                        </tr>
                      </thead>
                      <tbody>
                        <For each={v().entries}>
                          {(e) => (
                            <tr>
                              <td data-label="Trace">{e.trace}</td>
                              <td data-label="Sender">
                                {e.sender}
                                <Show when={e.sender_name}>
                                  <span class="cell-sub">{e.sender_name}</span>
                                </Show>
                              </td>
                              <td data-label="Amount" class="amount">{dollars(e.amount)}</td>
                              <td data-label="Original acct">{e.original_account}</td>
                              <td data-label="Fixed acct">{e.fixed_account}</td>
                              <td data-label="Cleaned acct">{e.cleaned_account}</td>
                              <td data-label="Release acct">{e.release_account || "—"}</td>
                              <td data-label="Receiver kept">
                                <Badge status={e.receiver_kept ? "pass" : "fail"} />
                              </td>
                            </tr>
                          )}
                        </For>
                      </tbody>
                    </table>
                    <Pagination
                      page={entryPage()}
                      pageSize={entryPageSize()}
                      total={v().entries_total}
                      onChange={(p, sz) => {
                        setEntryPage(p);
                        setEntryPageSize(sz);
                      }}
                    />
                  </div>
                </Show>
              </>
            )}
          </Show>

          <h2 class="section">Holds</h2>
          <Show when={s().holds_total > 0} fallback={<p class="empty">No holds for this file.</p>}>
            <HoldTable holds={s().holds} />
            <Pagination
              page={holdPage()}
              pageSize={holdPageSize()}
              total={s().holds_total}
              onChange={(p, sz) => {
                setHoldPage(p);
                setHoldPageSize(sz);
              }}
            />
          </Show>

          <h2 class="section">Jobs</h2>
          <Show when={s().jobs.length > 0} fallback={<p class="empty">No jobs yet.</p>}>
            <div class="table-wrap">
              <table class="responsive">
                <thead>
                  <tr>
                    <th>Kind</th>
                    <th>State</th>
                    <th>Failures</th>
                    <th>Last error</th>
                    <Show when={isSuperAdmin()}>
                      <th>Actions</th>
                    </Show>
                  </tr>
                </thead>
                <tbody>
                  <For each={s().jobs}>
                    {(j) => (
                      <tr>
                        <td data-label="Kind">{j.kind.replace(/_/g, " ")}</td>
                        <td data-label="State">
                          <Badge status={j.state} />
                        </td>
                        <td data-label="Failures">{j.failures}</td>
                        <td data-label="Last error" class="muted">{j.last_error || "—"}</td>
                        <Show when={isSuperAdmin()}>
                          <td data-label="Actions">
                            <Show when={j.state === "failed" || j.state === "in_progress"}>
                              <button class="btn btn-outline btn-sm" type="button" onClick={() => onRequeue(j)}>
                                Requeue
                              </button>
                            </Show>
                          </td>
                        </Show>
                      </tr>
                    )}
                  </For>
                </tbody>
              </table>
            </div>
          </Show>
        </>
      )}
    </Show>
  );
}
