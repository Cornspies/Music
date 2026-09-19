package server

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Single-use invite links.
//
// An admin mints a link for one friend; it works exactly once, expires on its
// own (24 hours by default), can be revoked, and records who used it. This
// sits alongside the static MUSIC_INVITE_CODE: an invite link always lets its
// holder register — even while sign-ups are otherwise closed — which is the
// point of handing one out.

const (
	defaultInviteHours = 24
	maxInviteHours     = 24 * 30
)

// Invite is an invite link as the admin console sees it.
type Invite struct {
	Code      string `json:"code"`
	Note      string `json:"note"`
	CreatedBy string `json:"createdBy"`
	CreatedAt string `json:"createdAt"`
	ExpiresAt string `json:"expiresAt"`
	// UsedBy is the username of the account that registered with it.
	UsedBy    *string `json:"usedBy"`
	UsedAt    *string `json:"usedAt"`
	RevokedAt *string `json:"revokedAt"`
	// Status is "pending", "used", "expired" or "revoked", worked out on the
	// server so every client agrees.
	Status string `json:"status"`
}

func newInviteCode() (string, error) {
	buf := make([]byte, 18)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	// URL-safe, no padding: it goes straight into a query string.
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func inviteStatus(inv *Invite, now time.Time) string {
	switch {
	case inv.RevokedAt != nil:
		return "revoked"
	case inv.UsedBy != nil:
		return "used"
	}
	if exp, err := time.Parse(time.RFC3339, inv.ExpiresAt); err == nil && !exp.After(now) {
		return "expired"
	}
	return "pending"
}

const inviteSelect = `
SELECT i.code, i.note, COALESCE(c.username, ''), i.created_at, i.expires_at,
       u.username, i.used_at, i.revoked_at
FROM invites i
LEFT JOIN users c ON c.id = i.created_by
LEFT JOIN users u ON u.id = i.used_by`

func scanInvite(scan func(dest ...any) error) (*Invite, error) {
	var inv Invite
	var usedBy, usedAt, revokedAt sql.NullString
	if err := scan(&inv.Code, &inv.Note, &inv.CreatedBy, &inv.CreatedAt, &inv.ExpiresAt,
		&usedBy, &usedAt, &revokedAt); err != nil {
		return nil, err
	}
	if usedBy.Valid {
		v := usedBy.String
		inv.UsedBy = &v
	}
	if usedAt.Valid && usedAt.String != "" {
		v := usedAt.String
		inv.UsedAt = &v
	}
	if revokedAt.Valid && revokedAt.String != "" {
		v := revokedAt.String
		inv.RevokedAt = &v
	}
	inv.Status = inviteStatus(&inv, time.Now())
	return &inv, nil
}

func (s *Server) getInvite(code string) (*Invite, error) {
	row := s.db.QueryRow(inviteSelect+` WHERE i.code = ?`, code)
	return scanInvite(row.Scan)
}

// consumeInvite marks a pending invite as used by the new account. It fails
// (without side effects) when the invite is missing, used, expired or
// revoked, so a second registration with the same link is refused.
func (s *Server) consumeInvite(code, userID string) error {
	res, err := s.db.Exec(`
		UPDATE invites SET used_by = ?, used_at = ?
		WHERE code = ? AND used_by IS NULL AND revoked_at IS NULL AND expires_at > ?`,
		userID, nowRFC3339(), code, nowRFC3339())
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return errors.New("invite is no longer valid")
	}
	return nil
}

// invitePending reports whether a code is a live single-use invite.
func (s *Server) invitePending(code string) bool {
	if code == "" {
		return false
	}
	inv, err := s.getInvite(code)
	return err == nil && inv.Status == "pending"
}

// ---- public: let the sign-up form validate a link before the user types ----

// InviteCheck is what GET /api/auth/invite/{code} returns. It reveals nothing
// beyond whether the link works and how long it has left — codes are 144
// random bits, so this can't be used to enumerate them.
type InviteCheck struct {
	Valid     bool   `json:"valid"`
	Status    string `json:"status"`
	ExpiresAt string `json:"expiresAt,omitempty"`
	// InvitedBy is the admin who made the link, so the page can say
	// "alice invited you".
	InvitedBy string `json:"invitedBy,omitempty"`
}

func (s *Server) handleCheckInvite(w http.ResponseWriter, r *http.Request) {
	// Same limiter as sign-in: a wrong code counts as a failed attempt.
	if blocked, retry := rateLimitAuth(r); blocked {
		w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())+1))
		writeErr(w, http.StatusTooManyRequests, "too many attempts, try again later")
		return
	}
	code := strings.TrimSpace(r.PathValue("code"))
	inv, err := s.getInvite(code)
	if errors.Is(err, sql.ErrNoRows) || code == "" {
		noteAuthFailure(r)
		writeJSON(w, http.StatusOK, InviteCheck{Valid: false, Status: "invalid"})
		return
	} else if err != nil {
		s.internalErr(w, "lookup invite", err)
		return
	}
	writeJSON(w, http.StatusOK, InviteCheck{
		Valid: inv.Status == "pending", Status: inv.Status, ExpiresAt: inv.ExpiresAt, InvitedBy: inv.CreatedBy,
	})
}

// ---- admin ----

func (s *Server) handleAdminListInvites(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(inviteSelect + ` ORDER BY i.created_at DESC LIMIT 500`)
	if err != nil {
		s.internalErr(w, "list invites", err)
		return
	}
	defer rows.Close()
	out := []*Invite{}
	for rows.Next() {
		inv, err := scanInvite(rows.Scan)
		if err != nil {
			s.internalErr(w, "scan invite", err)
			return
		}
		out = append(out, inv)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleAdminCreateInvite(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	var body struct {
		// Note is who it's for — shown in the list so the admin can tell
		// links apart and see who has signed up.
		Note string `json:"note"`
		// ExpiresHours defaults to 24; capped at 30 days.
		ExpiresHours int `json:"expiresHours"`
	}
	if err := jsonDecodeStrict(r, w, &body); err != nil {
		return
	}
	hours := body.ExpiresHours
	if hours <= 0 {
		hours = defaultInviteHours
	}
	if hours > maxInviteHours {
		hours = maxInviteHours
	}
	note := strings.TrimSpace(body.Note)
	if len(note) > 80 {
		note = note[:80]
	}
	code, err := newInviteCode()
	if err != nil {
		s.internalErr(w, "invite code", err)
		return
	}
	now := time.Now().UTC()
	if _, err := s.db.Exec(`INSERT INTO invites (code, note, created_by, created_at, expires_at) VALUES (?,?,?,?,?)`,
		code, note, u.ID, now.Format(time.RFC3339), now.Add(time.Duration(hours)*time.Hour).Format(time.RFC3339)); err != nil {
		s.internalErr(w, "create invite", err)
		return
	}
	inv, err := s.getInvite(code)
	if err != nil {
		s.internalErr(w, "read invite", err)
		return
	}
	writeJSON(w, http.StatusCreated, inv)
}

// handleAdminRevokeInvite invalidates a link. Used ones are kept (they're the
// record of who joined how); revoking one of those is a no-op.
func (s *Server) handleAdminRevokeInvite(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	res, err := s.db.Exec(`UPDATE invites SET revoked_at = ? WHERE code = ? AND revoked_at IS NULL AND used_by IS NULL`,
		nowRFC3339(), code)
	if err != nil {
		s.internalErr(w, "revoke invite", err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		var exists int
		_ = s.db.QueryRow(`SELECT COUNT(1) FROM invites WHERE code = ?`, code).Scan(&exists)
		if exists == 0 {
			writeErr(w, http.StatusNotFound, "invite not found")
			return
		}
	}
	inv, err := s.getInvite(code)
	if err != nil {
		s.internalErr(w, "read invite", err)
		return
	}
	writeJSON(w, http.StatusOK, inv)
}

// handleAdminDeleteInvite removes a link from the list entirely.
func (s *Server) handleAdminDeleteInvite(w http.ResponseWriter, r *http.Request) {
	if _, err := s.db.Exec(`DELETE FROM invites WHERE code = ?`, r.PathValue("code")); err != nil {
		s.internalErr(w, "delete invite", err)
		return
	}
	writeNoContent(w)
}
