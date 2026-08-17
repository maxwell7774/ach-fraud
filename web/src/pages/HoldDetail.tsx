import { createSignal, For, Show } from "solid-js";
import { createAsync, useParams, useAction, useSubmission, A, revalidate } from "@solidjs/router";
import { dollars, fmtDateTime, fmtDate, releaseStateLabel } from "../api";
import { Badge, Loading, QueryError } from "../components";
import { useFlash } from "../flash";
import { useConfirm } from "../confirm";
import { canReview } from "../user";
import { approveHoldAction, declineHoldAction, holdQuery, usePolling } from "../queries";
import { ArrowLeftIcon, CheckIcon, XIcon } from "../icons";

export default function HoldDetail() {
  usePolling("hold");
  const params = useParams<{ id: string }>();
  const { show: flash } = useFlash();
  const { confirm } = useConfirm();
  const [error, setError] = createSignal<unknown>();
  const hold = createAsync(() => { setError(undefined); return holdQuery(params.id).catch((e) => { setError(e); return undefined; }); });
  const approve = useAction(approveHoldAction);
  const decline = useAction(declineHoldAction);
  const approveSub = useSubmission(approveHoldAction);
  const declineSub = useSubmission(declineHoldAction);
  const busy = () => approveSub.pending || declineSub.pending;

  async function act(action: "approve" | "decline") {
    const h = hold();
    if (!h) return;
    const lines = [
      `Account: ${h.entry_receiver_account}`,
      h.entry_receiver_name ? `Name: ${h.entry_receiver_name}` : null,
      `Amount: ${dollars(h.entry_amount)}`,
      `RDFI: ${h.entry_rdfi}`,
      h.customer_id ? `Customer: ${h.customer_id}` : null,
      h.reason ? `Reason held: ${h.reason}` : null,
    ].filter(Boolean);
    if (h.group_size > 1) {
      lines.push(`\nPart of velocity group of ${h.group_size} — decision applies to all.`);
    }
    const msg = `${action === "approve" ? "Approve" : "Decline"} this hold?\n\n${lines.join("\n")}`;
    const res = await confirm(msg, {
      title: `${action === "approve" ? "Approve" : "Decline"} hold`,
      tone: action,
      confirmLabel: action === "approve" ? "Approve" : "Decline",
      showNote: true,
    });
    if (!res.ok) return;
    const submit = action === "approve" ? approve : decline;
    const result = await submit({ id: params.id, note: res.note });
    if (!result.ok) {
      flash("error", result.error ?? "failed");
      return;
    }
    flash("success", `${action === "approve" ? "Approved" : "Declined"} ${dollars(h.entry_amount)} to ${h.entry_receiver_account}`);
  }

  return (
    <Show when={hold()} fallback={<Show when={error()} fallback={<Loading label="Loading hold…" />}><QueryError error={error()} onRetry={() => revalidate("hold")} /></Show>}>
      {(h) => (
        <>
          <A class="back-link" href="/holds">
            <ArrowLeftIcon /> Holds
          </A>
          <h1>
            Hold on {h().entry_receiver_account} <Badge status={h().status} />
          </h1>

          <div class="detail-card">
            <h2>Entry</h2>
            <div class="detail-grid">
              <div class="key">Status</div>
              <div class="val">{h().status}</div>
              <div class="key">Why held</div>
              <div class="val">{h().reason || "—"}</div>
              <div class="key">Receiver</div>
              <div class="val">{h().entry_receiver_name || "—"}</div>
              <div class="key">Account</div>
              <div class="val">{h().entry_receiver_account}</div>
              <div class="key">RDFI</div>
              <div class="val">{h().entry_rdfi}</div>
              <div class="key">Amount</div>
              <div class="val">{dollars(h().entry_amount)}</div>
              <div class="key">Trace</div>
              <div class="val">{h().entry_trace}</div>
              <div class="key">Effective date</div>
              <div class="val">{fmtDate(h().effective_date)}</div>
              <div class="key">Source file</div>
              <div class="val">
                <A href={`/submissions/${h().submission_id}`} class="file-link">
                  {h().filename}
                </A>
              </div>
              <div class="key">Release</div>
              <div class="val">{releaseStateLabel(h().release_state)}</div>
              <div class="key">Created</div>
              <div class="val">{fmtDateTime(h().created_at)}</div>
            </div>
          </div>

          <Show when={h().velocity}>
            {(v) => (
              <div class="detail-card">
                <h2>Same-day velocity</h2>
                <p class="section-note">
                  Total funds this account received today across all files, and how much already
                  shipped before this hold tripped.
                </p>
                <div class="detail-grid">
                  <div class="key">Account</div>
                  <div class="val">{h().entry_receiver_account}</div>
                  <div class="key">Same-day total</div>
                  <div class="val">{dollars(v().total)}</div>
                  <div class="key">Already shipped before this hold</div>
                  <div class="val">
                    <Show when={v().prior > 0} fallback={<span class="muted">—</span>}>
                      <span class="leak">{dollars(v().prior)}</span>
                    </Show>
                  </div>
                  <div class="key">Held by this hold</div>
                  <div class="val">{dollars(v().held)}</div>
                </div>
              </div>
            )}
          </Show>

          <Show when={canReview() && (h().status === "pending" || h().status === "auto_declined")}>
            <div class="detail-card">
              <h2>Review</h2>
              <Show when={h().group_size > 1}>
                <p class="section-note">
                  This hold is part of a velocity group of {h().group_size}. Approving or
                  declining it applies to the whole group.
                </p>
              </Show>
              <div class="btn-group">
                <button class="btn btn-approve" type="button" disabled={busy()} onClick={() => act("approve")}>
                  <CheckIcon /> Approve
                </button>
                <button class="btn btn-decline" type="button" disabled={busy()} onClick={() => act("decline")}>
                  <XIcon /> Decline
                </button>
              </div>
            </div>
          </Show>

          <Show when={h().reviews.length > 0}>
            <h2 class="section">Review history</h2>
            <div class="table-wrap">
              <table class="responsive">
                <thead>
                  <tr>
                    <th>Action</th>
                    <th>Actor</th>
                    <th>Note</th>
                    <th>When</th>
                  </tr>
                </thead>
                <tbody>
                  <For each={h().reviews}>
                    {(r) => (
                      <tr>
                        <td data-label="Action">{r.action}</td>
                        <td data-label="Actor">{r.actor}</td>
                        <td data-label="Note">{r.note || "—"}</td>
                        <td data-label="When" class="muted">{fmtDateTime(r.created_at)}</td>
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
