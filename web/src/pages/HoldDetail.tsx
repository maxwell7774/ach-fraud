import { createSignal, For, Show } from "solid-js";
import { createAsync, useParams, useAction, useSubmission, A, revalidate } from "@solidjs/router";
import { dollars, fmtDateTime, fmtDate, human, releaseStateLabel } from "../api";
import { Badge, Loading, QueryError, Select } from "../components";
import { useFlash } from "../flash";
import { useConfirm } from "../confirm";
import { canReview } from "../user";
import { approveHoldAction, declineHoldAction, setStatusHoldAction, holdQuery, usePolling } from "../queries";
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
  const setStatusAction = useAction(setStatusHoldAction);
  const setStatusSub = useSubmission(setStatusHoldAction);
  const [targetStatus, setTargetStatus] = createSignal("");
  const busy = () => approveSub.pending || declineSub.pending || setStatusSub.pending;

  async function changeStatus(status: string) {
    const h = hold();
    if (!h) return;
    const choices =
      h.group_size > 1
        ? [
            { value: "hold", label: "This hold only" },
            { value: "group", label: `Whole velocity group (${h.group_size} holds)` },
          ]
        : undefined;
    const res = await confirm(`Set this hold's status to ${human(status)}? This only updates the record and how future files to this receiver are screened — it does not change any transmitted file.`, {
      title: `Set status: ${human(status)}`,
      tone: status === "approved" ? "approve" : status === "declined" || status === "auto_declined" ? "decline" : "neutral",
      confirmLabel: "Set status",
      showNote: true,
      choices,
    });
    if (!res.ok) return;
    const scope = res.choice === "group" ? "group" : "hold";
    const result = await setStatusAction({ id: params.id, status, note: res.note, scope });
    if (!result.ok) {
      flash("error", result.error ?? "failed");
      return;
    }
    flash("success", `Status set to ${human(status)} for ${h.entry_receiver_account}`);
  }

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

          <Show when={(h().group_holds?.length ?? 0) > 0}>
            <div class="detail-card">
              <h2>Velocity group ({h().group_size} holds)</h2>
              <p class="section-note">
                These holds share a release, so a status change applies to the whole group.
              </p>
              <div class="table-wrap">
                <table class="responsive">
                  <thead>
                    <tr>
                      <th>Status</th>
                      <th>Account</th>
                      <th>Amount</th>
                      <th>Receiver</th>
                    </tr>
                  </thead>
                  <tbody>
                    <For each={h().group_holds!}>
                      {(g) => (
                        <tr>
                          <td data-label="Status">
                            <Badge status={g.status} />
                          </td>
                          <td data-label="Account">
                            <A href={`/holds/${g.id}`} class="file-link">
                              {g.entry_receiver_account}
                            </A>
                          </td>
                          <td data-label="Amount" class="amount">
                            {dollars(g.entry_amount)}
                          </td>
                          <td data-label="Receiver">{g.entry_receiver_name || "—"}</td>
                        </tr>
                      )}
                    </For>
                  </tbody>
                </table>
              </div>
            </div>
          </Show>

          <Show when={canReview()}>
            <div class="detail-card">
              <h2>Review</h2>
              <Show when={h().status === "pending"}>
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
              </Show>
              <Show when={h().status !== "pending"}>
                <div class="override-row">
                  <p class="section-note">
                    {h().status === "auto_declined"
                      ? "This hold was auto-declined by the screening pipeline. You can override it to approved or declined for record-keeping."
                      : "Override the status for record-keeping. This changes how future files to this receiver are screened; it does not touch any transmitted file."}
                    {" "}If this hold is part of a velocity group you will be asked whether to apply the change to the whole group or just this hold.
                  </p>
                  <div class="btn-group">
                    <Select
                      value={targetStatus() || ""}
                      options={[
                        { value: "", label: "Set status…" },
                        { value: "approved", label: "Approved" },
                        { value: "declined", label: "Declined" },
                      ]}
                      onChange={setTargetStatus}
                    />
                    <button
                      class="btn"
                      type="button"
                      disabled={busy() || !targetStatus() || targetStatus() === h().status}
                      onClick={() => changeStatus(targetStatus())}
                    >
                      Apply
                    </button>
                  </div>
                </div>
              </Show>
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
