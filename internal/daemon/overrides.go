package daemon

import (
	"errors"
	"fmt"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/admin"
	"github.com/MNCloudwerksTechnology/obie/internal/audit"
	"github.com/MNCloudwerksTechnology/obie/internal/decision"
	"github.com/MNCloudwerksTechnology/obie/internal/sovereignty"
	"github.com/MNCloudwerksTechnology/obie/internal/store"
	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// storeOverrides serves the admin API's overrides from the store and
// records their changes in the audit log.
type storeOverrides struct {
	store store.Store
	now   func() time.Time
	// audit records set and deleted overrides; nil without audit log.
	audit *audit.Log
}

func (s storeOverrides) List() ([]admin.OverrideResponse, error) {
	list, err := s.store.Overrides(s.now())
	if err != nil {
		return nil, err
	}
	out := make([]admin.OverrideResponse, len(list))
	for i := range list {
		out[i] = overrideResponse(&list[i])
	}
	return out, nil
}

func (s storeOverrides) Set(ind obieproto.Indicator, action string, ttl time.Duration, note string) (admin.OverrideResponse, error) {
	now := s.now()
	o := store.Override{Indicator: ind, Action: store.Action(action), Note: note, CreatedAt: now}
	if ttl > 0 {
		o.ExpiresAt = now.Add(ttl)
	}
	if err := s.store.SetOverride(o); err != nil {
		if errors.Is(err, store.ErrInvalid) {
			return admin.OverrideResponse{}, fmt.Errorf("%w: %w", admin.ErrInvalid, err)
		}
		return admin.OverrideResponse{}, err
	}
	// The store normalizes the override; answer with what it holds.
	stored, err := s.store.Override(ind.Key(), now)
	if err != nil {
		return admin.OverrideResponse{}, err
	}
	s.audit.Write(audit.OverrideSet(&stored))
	return overrideResponse(&stored), nil
}

func (s storeOverrides) Delete(ind obieproto.Indicator) (bool, error) {
	// The action is only recorded; an override that cannot be read is
	// deleted all the same.
	old, _ := s.store.Override(ind.Key(), s.now())
	deleted, err := s.store.DeleteOverride(ind.Key())
	if deleted {
		s.audit.Write(audit.OverrideRemoved(ind, old.Action))
	}
	return deleted, err
}

func overrideResponse(o *store.Override) admin.OverrideResponse {
	resp := admin.OverrideResponse{Indicator: o.Indicator, Action: string(o.Action), Note: o.Note, CreatedAt: o.CreatedAt.UTC()}
	if !o.ExpiresAt.IsZero() {
		end := o.ExpiresAt.UTC()
		resp.ExpiresAt = &end
	}
	return resp
}

// sovereigntyResponse converts the effect of the allow-list and overrides
// on a decision into its admin API form.
func sovereigntyResponse(d *decision.Decision) *admin.SovereigntyResponse {
	r := d.Sovereignty
	if r.Effect == sovereignty.EffectNone {
		return &admin.SovereigntyResponse{Applied: true, Note: "no allow-list entry or override applies"}
	}
	resp := &admin.SovereigntyResponse{
		Applied:      true,
		Effect:       string(r.Effect),
		Rule:         string(r.Rule),
		Source:       string(r.Source),
		Match:        r.Match,
		Label:        r.Label,
		OverrideNote: r.Note,
		Note:         r.Reason,
	}
	if !r.ExpiresAt.IsZero() {
		end := r.ExpiresAt.UTC()
		resp.ExpiresAt = &end
	}
	return resp
}
