package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/27actions/ach/internal/achp"
	"github.com/27actions/ach/internal/domain"

	"github.com/google/uuid"
	"github.com/moov-io/ach"
)

type submissionDetail struct {
	domain.Submission
	Artifacts    []domain.Artifact    `json:"artifacts"`
	Holds        []domain.Hold        `json:"holds"`
	HoldsTotal   int64                `json:"holds_total"`
	Jobs         []domain.Job         `json:"jobs"`
	Verification *domain.Verification `json:"verification,omitempty"`
}

type submissionsPage struct {
	Submissions []domain.Submission `json:"submissions"`
	Total       int64               `json:"total"`
}

type artifactSum struct {
	Present bool   `json:"present"`
	Entries int    `json:"entries"`
	Total   int64  `json:"total"`
	Error   string `json:"error,omitempty"`
	Pruned  bool   `json:"pruned,omitempty"`
}

type entryMatch struct {
	Trace        string `json:"trace"`
	Amount       int64  `json:"amount"`
	Sender       string `json:"sender"`
	SenderName   string `json:"sender_name"`
	OriginalAcct string `json:"original_account"`
	FixedAcct    string `json:"fixed_account"`
	CleanedAcct  string `json:"cleaned_account"`
	ReleaseAcct  string `json:"release_account"`
	ReceiverKept bool   `json:"receiver_kept"`
}

type submissionVerify struct {
	Verified     bool                   `json:"verified"`
	Pruned       bool                   `json:"pruned"`
	Artifacts    map[string]artifactSum `json:"artifacts"`
	Issues       []string               `json:"issues"`
	Entries      []entryMatch           `json:"entries"`
	EntriesTotal int                    `json:"entries_total"`
}

func (s *Server) handleListSubmissions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	status := q.Get("status")
	search := q.Get("q")
	start := parseDate(q.Get("start_date"))
	end := parseDate(q.Get("end_date"))
	page, pageSize := pageParams(r)

	ctx := r.Context()
	total, err := s.deps.Store.CountSubmissionsFiltered(ctx, status, search, start, end)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	rows, err := s.deps.Store.ListSubmissionsFiltered(ctx, status, search, start, end, q.Get("sort"), q.Get("dir"), pageSize, (page-1)*pageSize)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, submissionsPage{Submissions: rows, Total: total})
}

func (s *Server) handleGetSubmission(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	ctx := r.Context()
	sub, err := s.deps.Store.GetSubmission(ctx, id)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			writeErr(w, http.StatusNotFound, err)
			return
		}
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	page, pageSize := pageParams(r)
	detail := submissionDetail{Submission: sub}
	if detail.Artifacts, err = s.deps.Store.ListArtifactsBySubmission(ctx, id); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	holds, err := s.deps.Store.ListHoldsBySubmission(ctx, id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	detail.Holds = pageSlice(holds, page, pageSize)
	detail.HoldsTotal = int64(len(holds))
	if detail.Jobs, err = s.deps.Store.ListJobsByRef(ctx, id); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if v, err := s.deps.Store.GetVerification(ctx, id); err == nil {
		detail.Verification = &v
	} else if !errors.Is(err, domain.ErrNotFound) {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

// handleVerifySubmission re-parses a submission's artifacts and checks that
// the amount, receiver, and sender hold across the original → fixed → cleaned
// chain (and the release legs, when present), surfacing any mismatch for
// review. Held entries are expected to redirect to the holding account in the
// cleaned file; a release leg returns the exact amount to the real receiver.
func (s *Server) handleVerifySubmission(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	ctx := r.Context()
	arts, err := s.deps.Store.ListArtifactsBySubmission(ctx, id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	sums := map[string]artifactSum{}
	files := map[domain.ArtifactKind]*ach.File{}
	var releases []*ach.File
	pruned := map[domain.ArtifactKind]bool{}
	for _, a := range arts {
		sum := artifactSum{Present: true}
		if a.State == domain.ArtifactPruned {
			// Bytes are intentionally gone after the retention window; report
			// it as pruned rather than a failure to read.
			sum.Present = false
			sum.Pruned = true
			sums[string(a.Kind)] = sum
			pruned[a.Kind] = true
			continue
		}
		data, err := s.deps.Files.Get(ctx, a.Checksum)
		if err != nil {
			sum.Error = err.Error()
			sums[string(a.Kind)] = sum
			continue
		}
		f, err := achp.Read(data, true)
		if err != nil {
			sum.Error = err.Error()
			sums[string(a.Kind)] = sum
			continue
		}
		sum.Entries = achp.TotalEntries(f)
		sum.Total = artifactTotal(f)
		sums[string(a.Kind)] = sum
		if a.Kind == domain.ArtifactRelease {
			releases = append(releases, f)
		} else {
			files[a.Kind] = f
		}
	}

	orig, hasOrig := files[domain.ArtifactOriginal]
	fixed, hasFixed := files[domain.ArtifactFixed]
	cleaned, hasCleaned := files[domain.ArtifactCleaned]

	var issues []string
	// Verification needs the bytes; a pruned artifact means it simply cannot
	// run, which is expected after retention, not an error.
	for _, kind := range []domain.ArtifactKind{
		domain.ArtifactOriginal, domain.ArtifactFixed, domain.ArtifactCleaned,
	} {
		if pruned[kind] {
			issues = append(issues, fmt.Sprintf("%s was pruned; verification unavailable", kind))
		}
	}
	if hasOrig && hasFixed {
		if err := achp.VerifySame(orig, fixed); err != nil {
			issues = append(issues, "fixed does not match original: "+err.Error())
		}
	}
	if hasFixed && hasCleaned {
		if err := amountsMatch(fixed, cleaned); err != nil {
			issues = append(issues, "cleaned amounts do not match fixed: "+err.Error())
		}
	}
	issues = append(issues, senderIssues(orig, fixed, cleaned)...)
	if hasCleaned && !hasFixed {
		issues = append(issues, "cleaned artifact exists without a fixed artifact")
	}
	if !hasCleaned && hasFixed {
		issues = append(issues, "submission has not been processed (no cleaned artifact)")
	}

	page, pageSize := pageParams(r)
	var entries []entryMatch
	if hasOrig && hasFixed {
		entries = compareEntries(orig, fixed, cleaned, releases, s.deps.Policy.HoldingAccount)
	}
	// Every held entry must have a matching release leg when release bytes
	// exist; a held entry with no leg means the release is missing a return.
	if len(releases) > 0 {
		for _, em := range entries {
			if em.CleanedAcct == s.deps.Policy.HoldingAccount && em.ReleaseAcct == "" {
				issues = append(issues, fmt.Sprintf("entry %s held but missing a release leg", em.Trace))
			}
		}
	}
	total := len(entries)
	entries = pageSlice(entries, page, pageSize)
	if issues == nil {
		issues = []string{}
	}
	if entries == nil {
		entries = []entryMatch{}
	}

	writeJSON(w, http.StatusOK, submissionVerify{
		Verified:     len(issues) == 0 && hasCleaned,
		Pruned:       len(pruned) > 0,
		Artifacts:    sums,
		Issues:       issues,
		Entries:      entries,
		EntriesTotal: total,
	})
}

func artifactTotal(f *ach.File) int64 {
	var total int64
	for _, b := range f.Batches {
		for _, e := range b.GetEntries() {
			total += int64(e.Amount)
		}
	}
	return total
}

func amountsMatch(fixed, cleaned *ach.File) error {
	if len(cleaned.Batches) != len(fixed.Batches) {
		return fmt.Errorf("batch count %d != %d", len(cleaned.Batches), len(fixed.Batches))
	}
	for bi := range fixed.Batches {
		fe, ce := fixed.Batches[bi].GetEntries(), cleaned.Batches[bi].GetEntries()
		if len(fe) != len(ce) {
			return fmt.Errorf("batch %d entry count %d != %d", bi+1, len(ce), len(fe))
		}
		for ei := range fe {
			if fe[ei].Amount != ce[ei].Amount {
				return fmt.Errorf("batch %d entry %d amount %d != %d", bi+1, ei+1, ce[ei].Amount, fe[ei].Amount)
			}
		}
	}
	return nil
}

// compareEntries builds one row per fixed entry showing the money's
// destination account across the chain: original → fixed → cleaned, plus the
// release leg (the real receiver again) when a held entry has one. Held entries
// are identified by their redirect to the holding account in the cleaned file.
func compareEntries(orig, fixed, cleaned *ach.File, releases []*ach.File, holdingAccount string) []entryMatch {
	legs := indexReleaseLegs(releases)
	var out []entryMatch
	for bi, fb := range fixed.Batches {
		if bi >= len(orig.Batches) {
			break
		}
		fe := fb.GetEntries()
		oe := orig.Batches[bi].GetEntries()
		var ce []*ach.EntryDetail
		if cleaned != nil && bi < len(cleaned.Batches) {
			ce = cleaned.Batches[bi].GetEntries()
		}
		sender := fb.GetHeader().CompanyIdentification
		senderName := fb.GetHeader().CompanyName
		for ei := range fe {
			em := entryMatch{
				Trace:        fe[ei].TraceNumberField(),
				Amount:       int64(fe[ei].Amount),
				Sender:       sender,
				SenderName:   senderName,
				OriginalAcct: strings.TrimSpace(oe[ei].DFIAccountNumber),
				FixedAcct:    strings.TrimSpace(fe[ei].DFIAccountNumber),
				ReceiverKept: oe[ei].DFIAccountNumber == fe[ei].DFIAccountNumber,
			}
			if ei < len(ce) {
				em.CleanedAcct = strings.TrimSpace(ce[ei].DFIAccountNumber)
			}
			if em.CleanedAcct == holdingAccount {
				em.ReleaseAcct = popReleaseLeg(legs, fe[ei])
			}
			out = append(out, em)
		}
	}
	return out
}

// indexReleaseLegs indexes release legs by their receiver (amount, routing
// number, account, tax id) so held entries can be matched to the leg that
// returns their funds, the same key VerifyRelease uses.
func indexReleaseLegs(releases []*ach.File) map[string][]*ach.EntryDetail {
	idx := map[string][]*ach.EntryDetail{}
	for _, rel := range releases {
		for _, b := range rel.Batches {
			for _, leg := range b.GetEntries() {
				key := releaseLegKey(leg)
				idx[key] = append(idx[key], leg)
			}
		}
	}
	return idx
}

func releaseLegKey(leg *ach.EntryDetail) string {
	return fmt.Sprintf("%d|%s|%s|%s", leg.Amount, leg.RDFIIdentification+leg.CheckDigit, strings.TrimSpace(leg.DFIAccountNumber), leg.IdentificationNumber)
}

// popReleaseLeg matches a held fixed entry to one release leg, consuming it so
// identical legs do not double-match. It returns the leg's destination account,
// or "" when no leg exists.
func popReleaseLeg(idx map[string][]*ach.EntryDetail, entry *ach.EntryDetail) string {
	key := fmt.Sprintf("%d|%s|%s|%s", entry.Amount, entry.RDFIIdentification+entry.CheckDigit, strings.TrimSpace(entry.DFIAccountNumber), entry.IdentificationNumber)
	list := idx[key]
	if len(list) == 0 {
		return ""
	}
	idx[key] = list[1:]
	return strings.TrimSpace(list[0].DFIAccountNumber)
}

// senderIssues reports batch-level sender (customer id + company name)
// mismatches across the original → fixed → cleaned chain.
func senderIssues(orig, fixed, cleaned *ach.File) []string {
	var out []string
	if fixed == nil {
		return out
	}
	for bi, fb := range fixed.Batches {
		id, name := fb.GetHeader().CompanyIdentification, fb.GetHeader().CompanyName
		if orig != nil && bi < len(orig.Batches) {
			oh := orig.Batches[bi].GetHeader()
			if oh.CompanyIdentification != id || oh.CompanyName != name {
				out = append(out, fmt.Sprintf("batch %d sender differs from original", bi+1))
			}
		}
		if cleaned != nil && bi < len(cleaned.Batches) {
			ch := cleaned.Batches[bi].GetHeader()
			if ch.CompanyIdentification != id || ch.CompanyName != name {
				out = append(out, fmt.Sprintf("batch %d sender differs from cleaned", bi+1))
			}
		}
	}
	return out
}
