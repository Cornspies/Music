package server

import (
	"compress/gzip"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Listen together: someone hosts a session and shares its invite link, and
// everyone who joins hears the same queue at the same moment. The server keeps
// the timeline (which entry plays, and where it was at a given time); every
// device plays the audio itself and follows along through a long poll that
// answers as soon as anything changes. Nobody pauses for everyone: a session
// plays whenever there's something to play, pausing stops one device, and
// playing again catches up. Sessions live in memory, so a restart ends them.

const (
	maxListenGuests     = 20
	defaultListenGuests = 5
	// Upcoming entries. Every device gets the whole queue when it changes,
	// gzipped when it can take that (see writeListenState).
	maxListenQueue = 1000
	// Played entries kept for "previous"; the rest go in batches, since each
	// trim sends the queue to everyone again.
	listenHistoryKeep = 20
	listenHistoryTrim = 30

	listenPollWait = 25 * time.Second
	// A member who polled this recently is listening right now.
	listenOnlineWindow = 45 * time.Second
	// A session ends by itself once the host has been away this long, so a
	// phone changing networks or an app restarting doesn't end it. Every
	// device that follows a session checks in at least once a listenPollWait.
	listenHostTimeout = 5 * time.Minute
	// How long a session that ended that way is remembered, so the devices
	// still following it can say why.
	listenTimedOutMemory = 15 * time.Minute
	// The server moves on by itself this long after a track should have
	// ended; a device reporting the end normally moves everyone on sooner.
	listenEndGrace = 2 * time.Second
	// A report that a track ended is only believed this close to its end, so
	// it can't be used to skip.
	listenEndSlack = 3.0
)

// Why a request about a session gets no answer about it.
const (
	listenGoneEnded    = "ended"
	listenGoneTimedOut = "timeout" // the host was away for listenHostTimeout
	listenGoneLeft     = "left"
	listenGoneRemoved  = "removed"
)

type listenMember struct {
	user     Uploader
	lastSeen time.Time
	online   bool // as last announced to the others
}

type listenEntry struct {
	id       string
	trackID  string
	duration float64
	addedBy  Uploader
}

type listenSession struct {
	id                 string
	code               string // the invite link's
	hostID             string
	capacity           int // guests, not counting the host
	everyoneCanControl bool

	members map[string]*listenMember
	order   []string          // member IDs, in the order they joined
	gone    map[string]string // former members: listenGoneLeft or listenGoneRemoved

	entries  []listenEntry
	current  int     // index into entries; -1 until something is queued
	position float64 // seconds into the current entry at time at
	at       time.Time
	finished bool // the queue ran out; a song added now starts right away

	version      int64
	queueVersion int64
	changed      chan struct{} // closed on every change
	timerGen     int
}

type listenHub struct {
	mu       sync.Mutex
	sessions map[string]*listenSession
	byCode   map[string]*listenSession
	byUser   map[string]*listenSession // every member, the host included
	timedOut map[string]time.Time      // sessions that ended with the host away, and when
}

func newListenHub() *listenHub {
	return &listenHub{
		sessions: map[string]*listenSession{},
		byCode:   map[string]*listenSession{},
		byUser:   map[string]*listenSession{},
		timedOut: map[string]time.Time{},
	}
}

type ListenMember struct {
	ID        string `json:"id"`
	Username  string `json:"username"`
	HasAvatar bool   `json:"hasAvatar"`
	IsHost    bool   `json:"isHost"`
	Online    bool   `json:"online"`
}

type ListenEntry struct {
	ID      string   `json:"id"`
	Track   Track    `json:"track"`
	AddedBy Uploader `json:"addedBy"`
}

// ListenState is a session as one member sees it. Position is where the
// current entry is at the moment the answer was written.
type ListenState struct {
	ID                 string         `json:"id"`
	Code               string         `json:"code,omitempty"` // for the host, who shares the link
	Host               Uploader       `json:"host"`
	IsHost             bool           `json:"isHost"`
	CanControl         bool           `json:"canControl"`
	EveryoneCanControl bool           `json:"everyoneCanControl"`
	Capacity           int            `json:"capacity"`
	Members            []ListenMember `json:"members"`
	Version            int64          `json:"version"`
	QueueVersion       int64          `json:"queueVersion"`
	// Left out when the device already has this queue version.
	Entries *[]ListenEntry `json:"entries,omitempty"`
	Current string         `json:"current"`
	// False only while there's nothing to play: nobody pauses for everyone.
	Playing  bool    `json:"playing"`
	Position float64 `json:"position"`
}

type ListenInvite struct {
	ID         string             `json:"id"`
	Host       Uploader           `json:"host"`
	Guests     int                `json:"guests"`
	Capacity   int                `json:"capacity"`
	Full       bool               `json:"full"`
	Joined     bool               `json:"joined"`
	NowPlaying *ListenInviteTrack `json:"nowPlaying,omitempty"`
}

type ListenInviteTrack struct {
	Title  string `json:"title"`
	Artist string `json:"artist"`
}

type listenSnapshot struct {
	state   ListenState
	entries []listenEntry // nil when the viewer already has this queue version
}

func newListenCode() (string, error) {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// newEntryID is random rather than counted, so that a device moving on to
// another session never mistakes an entry there for the one it was playing.
func newEntryID() string {
	buf := make([]byte, 8)
	_, _ = rand.Read(buf)
	return base64.RawURLEncoding.EncodeToString(buf)
}

func clampGuests(n int) int {
	if n <= 0 {
		return defaultListenGuests
	}
	return min(n, maxListenGuests)
}

// playing: the session plays whenever there's something to play.
func (ls *listenSession) playing() bool { return ls.current >= 0 && !ls.finished }

func (ls *listenSession) positionAt(now time.Time) float64 {
	if ls.current < 0 {
		return 0
	}
	pos := ls.position
	if ls.playing() {
		pos += now.Sub(ls.at).Seconds()
	}
	if d := ls.entries[ls.current].duration; d > 0 && pos > d {
		pos = d
	}
	return max(pos, 0)
}

func (ls *listenSession) canControl(userID string) bool {
	return userID == ls.hostID || ls.everyoneCanControl
}

func (ls *listenSession) guests() int { return len(ls.members) - 1 }

func (ls *listenSession) upcoming() int { return len(ls.entries) - (ls.current + 1) }

func (ls *listenSession) indexOf(entryID string) int {
	return slices.IndexFunc(ls.entries, func(e listenEntry) bool { return e.id == entryID })
}

func (ls *listenSession) startEntry(i int, now time.Time) {
	ls.current = i
	ls.position, ls.at = 0, now
	ls.finished = false
}

// seek also starts a queue that ran out over, at that point of its last song.
func (ls *listenSession) seek(pos float64, now time.Time) {
	if ls.current < 0 {
		return
	}
	if d := ls.entries[ls.current].duration; d > 0 {
		pos = min(pos, d)
	}
	ls.position, ls.at = max(pos, 0), now
	ls.finished = false
}

// advance moves on to the next entry, or at the end of the queue stops at the
// start of the last one. It reports whether the queue itself changed.
func (ls *listenSession) advance(now time.Time) bool {
	if ls.current < 0 {
		return false
	}
	if ls.current+1 >= len(ls.entries) {
		ls.position, ls.at = 0, now
		ls.finished = true
		return false
	}
	ls.startEntry(ls.current+1, now)
	if ls.current < listenHistoryKeep+listenHistoryTrim {
		return false
	}
	drop := ls.current - listenHistoryKeep
	ls.entries = slices.Delete(ls.entries, 0, drop)
	ls.current -= drop
	return true
}

func (ls *listenSession) previous(now time.Time) {
	if ls.current < 0 {
		return
	}
	if ls.current == 0 || ls.positionAt(now) > 3 {
		ls.position, ls.at = 0, now
		ls.finished = false
		return
	}
	ls.startEntry(ls.current-1, now)
}

// ended is a device saying the entry played to its end. The first report moves
// everyone on; later ones, and ones long before the end, change nothing.
func (ls *listenSession) ended(entryID string, now time.Time) (changed, queueChanged bool) {
	if !ls.playing() || ls.entries[ls.current].id != entryID {
		return false, false
	}
	if d := ls.entries[ls.current].duration; d > 0 && ls.positionAt(now) < d-listenEndSlack {
		return false, false
	}
	return true, ls.advance(now)
}

// add queues entries at the end, after the current one ("next"), or after the
// current one and starts them ("now"). With nothing to play they start anyway.
func (ls *listenSession) add(entries []listenEntry, mode string, now time.Time) {
	at := len(ls.entries)
	if mode != "end" && ls.current >= 0 {
		at = ls.current + 1
	}
	for i := range entries {
		entries[i].id = newEntryID()
	}
	ls.entries = slices.Insert(ls.entries, at, entries...)
	if mode == "now" || ls.current < 0 || ls.finished {
		ls.startEntry(at, now)
	}
}

func (ls *listenSession) snapshot(viewerID string, knownQueue int64, now time.Time) listenSnapshot {
	st := ListenState{
		ID:                 ls.id,
		Host:               ls.members[ls.hostID].user,
		IsHost:             viewerID == ls.hostID,
		CanControl:         ls.canControl(viewerID),
		EveryoneCanControl: ls.everyoneCanControl,
		Capacity:           ls.capacity,
		Members:            make([]ListenMember, 0, len(ls.order)),
		Version:            ls.version,
		QueueVersion:       ls.queueVersion,
		Playing:            ls.playing(),
		Position:           math.Round(ls.positionAt(now)*1000) / 1000,
	}
	if st.IsHost {
		st.Code = ls.code
	}
	if ls.current >= 0 {
		st.Current = ls.entries[ls.current].id
	}
	for _, id := range ls.order {
		m := ls.members[id]
		st.Members = append(st.Members, ListenMember{
			ID:        id,
			Username:  m.user.Username,
			HasAvatar: m.user.HasAvatar,
			IsHost:    id == ls.hostID,
			Online:    now.Sub(m.lastSeen) < listenOnlineWindow,
		})
	}
	snap := listenSnapshot{state: st}
	if knownQueue != ls.queueVersion {
		snap.entries = append([]listenEntry{}, ls.entries...)
	}
	return snap
}

// touch tells everyone following the session that it changed, and plans the
// move to the next entry. The caller holds h.mu.
func (h *listenHub) touch(ls *listenSession, queueChanged bool) {
	ls.version++
	if queueChanged {
		ls.queueVersion++
	}
	close(ls.changed)
	ls.changed = make(chan struct{})
	h.schedule(ls)
}

func (h *listenHub) schedule(ls *listenSession) {
	ls.timerGen++
	if !ls.playing() {
		return
	}
	d := ls.entries[ls.current].duration
	if d <= 0 {
		return
	}
	wait := time.Duration((d-ls.positionAt(time.Now()))*float64(time.Second)) + listenEndGrace
	gen := ls.timerGen
	time.AfterFunc(max(wait, 0), func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if ls.timerGen != gen || h.sessions[ls.id] != ls {
			return
		}
		h.touch(ls, ls.advance(time.Now()))
	})
}

func (h *listenHub) end(ls *listenSession) {
	delete(h.sessions, ls.id)
	delete(h.byCode, ls.code)
	for id := range ls.members {
		if h.byUser[id] == ls {
			delete(h.byUser, id)
		}
	}
	ls.timerGen++
	// Wakes everyone polling, who then find the session gone.
	close(ls.changed)
	ls.changed = make(chan struct{})
}

func (h *listenHub) removeMember(ls *listenSession, userID, reason string) {
	delete(ls.members, userID)
	ls.order = slices.DeleteFunc(ls.order, func(id string) bool { return id == userID })
	ls.gone[userID] = reason
	if h.byUser[userID] == ls {
		delete(h.byUser, userID)
	}
	h.touch(ls, false)
}

// leave takes a user out of whatever session they're in; a host leaving ends it.
func (h *listenHub) leave(userID string) {
	ls := h.byUser[userID]
	switch {
	case ls == nil:
	case ls.hostID == userID:
		h.end(ls)
	default:
		h.removeMember(ls, userID, listenGoneLeft)
	}
}

// forgetListener takes a deleted account out of its session.
func (s *Server) forgetListener(userID string) {
	s.listen.mu.Lock()
	defer s.listen.mu.Unlock()
	s.listen.leave(userID)
}

// member finds the session a request is about and answers for it when the user
// isn't (or is no longer) in it. The caller holds h.mu.
func (h *listenHub) member(w http.ResponseWriter, id, userID string) (*listenSession, bool) {
	ls := h.sessions[id]
	if ls == nil {
		if _, ok := h.timedOut[id]; ok {
			writeListenGone(w, listenGoneTimedOut)
		} else {
			// Unknown IDs included: a restart forgets every session.
			writeListenGone(w, listenGoneEnded)
		}
		return nil, false
	}
	if _, ok := ls.members[userID]; !ok {
		reason := ls.gone[userID]
		if reason == "" {
			reason = listenGoneLeft
		}
		writeListenGone(w, reason)
		return nil, false
	}
	return ls, true
}

func writeListenGone(w http.ResponseWriter, reason string) {
	msg := "this listening session has ended"
	switch reason {
	case listenGoneTimedOut:
		msg = "this listening session ended because the host was away too long"
	case listenGoneLeft:
		msg = "you're not in this listening session any more"
	case listenGoneRemoved:
		msg = "the host removed you from this listening session"
	}
	writeJSON(w, http.StatusNotFound, map[string]string{"error": msg, "reason": reason})
}

func writeListenForbidden(w http.ResponseWriter, ls *listenSession) {
	writeErr(w, http.StatusForbidden, "only "+ls.members[ls.hostID].user.Username+" can do that in this session")
}

// sweep ends sessions whose host has been away for listenHostTimeout, and tells
// members when someone comes or goes offline.
func (h *listenHub) sweep(now time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, at := range h.timedOut {
		if now.Sub(at) > listenTimedOutMemory {
			delete(h.timedOut, id)
		}
	}
	for _, ls := range h.sessions {
		if now.Sub(ls.members[ls.hostID].lastSeen) > listenHostTimeout {
			log.Printf("listen together: session %s ended, the host was away for %s", ls.id, listenHostTimeout)
			h.end(ls)
			h.timedOut[ls.id] = now
			continue
		}
		flipped := false
		for _, m := range ls.members {
			online := now.Sub(m.lastSeen) < listenOnlineWindow
			if online != m.online {
				m.online = online
				flipped = true
			}
		}
		if flipped {
			h.touch(ls, false)
		}
	}
}

// seen notes that a member's device was there until now.
func (h *listenHub) seen(id, userID string, now time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if ls := h.sessions[id]; ls != nil {
		if m := ls.members[userID]; m != nil {
			m.lastSeen = now
		}
	}
}

func (s *Server) startListenSweeper() {
	go func() {
		defer logPanic("listen sweeper")
		for range time.Tick(15 * time.Second) {
			s.listen.sweep(time.Now())
		}
	}()
}

// writeListenState fills in the queue's tracks as the viewer sees them (plays,
// favorite) and answers with the state. It runs without h.mu held.
func (s *Server) writeListenState(w http.ResponseWriter, r *http.Request, code int, viewerID string, snap listenSnapshot) {
	st := snap.state
	if snap.entries != nil {
		ids := make([]string, 0, len(snap.entries))
		seen := map[string]bool{}
		for _, e := range snap.entries {
			if !seen[e.trackID] {
				seen[e.trackID] = true
				ids = append(ids, e.trackID)
			}
		}
		tracks, err := s.tracksByIDs(viewerID, ids)
		if err != nil {
			s.internalErr(w, "load session queue", err)
			return
		}
		byID := make(map[string]Track, len(tracks))
		for _, t := range tracks {
			byID[t.ID] = t
		}
		// A track deleted since it was queued drops out.
		entries := make([]ListenEntry, 0, len(snap.entries))
		for _, e := range snap.entries {
			if t, ok := byID[e.trackID]; ok {
				entries = append(entries, ListenEntry{ID: e.id, Track: t, AddedBy: e.addedBy})
			}
		}
		st.Entries = &entries
	}
	w.Header().Set("Cache-Control", "no-store")
	// A full queue runs to a megabyte of JSON, and every device gets it on
	// each change; compressed it's a tenth of that. Browsers, OkHttp and
	// NSURLSession all ask for gzip and unpack it themselves.
	if st.Entries != nil && strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Add("Vary", "Accept-Encoding")
		w.WriteHeader(code)
		gz := gzip.NewWriter(w)
		_ = json.NewEncoder(gz).Encode(st)
		_ = gz.Close()
		return
	}
	writeJSON(w, code, st)
}

func knownQueueVersion(r *http.Request) int64 {
	v, err := strconv.ParseInt(r.URL.Query().Get("queue"), 10, 64)
	if err != nil {
		return -1
	}
	return v
}

// libraryOf is the library a user plays from (see demo.go): their own for a
// demo account, the shared one ("") for everyone else.
func (s *Server) libraryOf(userID string) (string, error) {
	var scope string
	err := s.db.QueryRow(`SELECT `+demoScope("?"), userID).Scan(&scope)
	return scope, err
}

// canJoinListen: everyone in a session has to be able to play what the host
// queues, so only accounts with the host's library join (and admins, who see
// every library).
func (s *Server) canJoinListen(u *User, hostID string) (bool, error) {
	if u.IsAdmin() || u.ID == hostID {
		return true, nil
	}
	mine, err := s.libraryOf(u.ID)
	if err != nil {
		return false, err
	}
	host, err := s.libraryOf(hostID)
	return mine == host, err
}

// listenEntries makes entries for the given tracks, in order, leaving out any
// the adder can't see or that aren't in the host's library.
func (s *Server) listenEntries(adder Uploader, hostID string, ids []string) ([]listenEntry, error) {
	unique := make([]string, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		if id = strings.TrimSpace(id); id != "" && !seen[id] {
			seen[id] = true
			unique = append(unique, id)
		}
	}
	if len(unique) == 0 {
		return nil, nil
	}
	marks := strings.Repeat("?,", len(unique))
	args := make([]any, 0, len(unique)+2)
	for _, id := range unique {
		args = append(args, id)
	}
	args = append(args, adder.ID, hostID)
	rows, err := s.db.Query(`SELECT t.id, t.duration FROM tracks t
		WHERE t.id IN (`+marks[:len(marks)-1]+`) AND `+visibleTo("t.user_id")+` AND `+sameLibrary("t.user_id"), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	durations := make(map[string]float64, len(unique))
	for rows.Next() {
		var id string
		var d float64
		if err := rows.Scan(&id, &d); err != nil {
			return nil, err
		}
		durations[id] = d
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]listenEntry, 0, len(ids))
	for _, id := range ids {
		if d, ok := durations[strings.TrimSpace(id)]; ok {
			out = append(out, listenEntry{trackID: strings.TrimSpace(id), duration: d, addedBy: adder})
		}
	}
	return out, nil
}

func (s *Server) uploaderByID(id string) (Uploader, error) {
	u, err := s.publicUserByID(id)
	return Uploader{ID: u.ID, Username: u.Username, HasAvatar: u.HasAvatar}, err
}

func (s *Server) handleListenCurrent(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	h := s.listen
	h.mu.Lock()
	ls := h.byUser[u.ID]
	if ls == nil {
		h.mu.Unlock()
		writeNoContent(w)
		return
	}
	now := time.Now()
	ls.members[u.ID].lastSeen = now
	snap := ls.snapshot(u.ID, knownQueueVersion(r), now)
	h.mu.Unlock()
	s.writeListenState(w, r, http.StatusOK, u.ID, snap)
}

type listenCreateRequest struct {
	Capacity           int     `json:"capacity"`
	EveryoneCanControl bool    `json:"everyoneCanControl"`
	TrackID            string  `json:"trackId"`
	Position           float64 `json:"position"`
	// What the host had queued after the current track.
	Upcoming []string `json:"upcoming"`
}

func (s *Server) handleListenCreate(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	var body listenCreateRequest
	if err := jsonDecodeStrict(r, w, &body); err != nil {
		return
	}
	host, err := s.uploaderByID(u.ID)
	if err != nil {
		s.internalErr(w, "lookup host", err)
		return
	}
	body.TrackID = strings.TrimSpace(body.TrackID)
	ids := body.Upcoming[:min(len(body.Upcoming), maxListenQueue)]
	if body.TrackID != "" {
		ids = append([]string{body.TrackID}, ids...)
	}
	entries, err := s.listenEntries(host, u.ID, ids)
	if err != nil {
		s.internalErr(w, "load tracks", err)
		return
	}
	code, err := newListenCode()
	if err != nil {
		s.internalErr(w, "make invite code", err)
		return
	}

	now := time.Now()
	ls := &listenSession{
		id:                 uuid.NewString(),
		code:               code,
		hostID:             u.ID,
		capacity:           clampGuests(body.Capacity),
		everyoneCanControl: body.EveryoneCanControl,
		members:            map[string]*listenMember{u.ID: {user: host, lastSeen: now, online: true}},
		order:              []string{u.ID},
		gone:               map[string]string{},
		current:            -1,
		at:                 now,
		version:            1,
		queueVersion:       1,
		changed:            make(chan struct{}),
	}
	if len(entries) > 0 {
		// Starting it starts the music, from where the host was.
		ls.add(entries, "end", now)
		if entries[0].trackID == body.TrackID {
			ls.seek(body.Position, now)
		}
	}

	h := s.listen
	h.mu.Lock()
	h.leave(u.ID)
	h.sessions[ls.id] = ls
	h.byCode[ls.code] = ls
	h.byUser[u.ID] = ls
	h.schedule(ls)
	snap := ls.snapshot(u.ID, -1, now)
	h.mu.Unlock()
	s.writeListenState(w, r, http.StatusCreated, u.ID, snap)
}

func (s *Server) handleListenInvite(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	h := s.listen
	h.mu.Lock()
	ls := h.byCode[r.PathValue("code")]
	if ls == nil {
		h.mu.Unlock()
		writeErr(w, http.StatusNotFound, "this listening session has ended")
		return
	}
	_, joined := ls.members[u.ID]
	out := ListenInvite{
		ID:       ls.id,
		Host:     ls.members[ls.hostID].user,
		Guests:   ls.guests(),
		Capacity: ls.capacity,
		Full:     ls.guests() >= ls.capacity,
		Joined:   joined,
	}
	hostID := ls.hostID
	current := ""
	if ls.current >= 0 {
		current = ls.entries[ls.current].trackID
	}
	h.mu.Unlock()

	allowed, err := s.canJoinListen(u, hostID)
	if err != nil {
		s.internalErr(w, "check library", err)
		return
	}
	if !allowed {
		writeErr(w, http.StatusForbidden, "this account can't join that listening session")
		return
	}
	if current != "" {
		var np ListenInviteTrack
		if err := s.db.QueryRow(`SELECT title, artist FROM tracks WHERE id = ?`, current).
			Scan(&np.Title, &np.Artist); err == nil {
			out.NowPlaying = &np
		} else if !errors.Is(err, sql.ErrNoRows) {
			logErr("load now playing", err)
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleListenJoin(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	code := r.PathValue("code")
	me, err := s.uploaderByID(u.ID)
	if err != nil {
		s.internalErr(w, "lookup user", err)
		return
	}
	h := s.listen
	h.mu.Lock()
	ls := h.byCode[code]
	hostID := ""
	if ls != nil {
		hostID = ls.hostID
	}
	h.mu.Unlock()
	if ls == nil {
		writeErr(w, http.StatusNotFound, "this listening session has ended")
		return
	}
	allowed, err := s.canJoinListen(u, hostID)
	if err != nil {
		s.internalErr(w, "check library", err)
		return
	}
	if !allowed {
		writeErr(w, http.StatusForbidden, "this account can't join that listening session")
		return
	}

	h.mu.Lock()
	if h.byCode[code] != ls {
		h.mu.Unlock()
		writeErr(w, http.StatusNotFound, "this listening session has ended")
		return
	}
	now := time.Now()
	if m, ok := ls.members[u.ID]; ok {
		m.lastSeen = now
	} else {
		if ls.gone[u.ID] == listenGoneRemoved {
			h.mu.Unlock()
			writeErr(w, http.StatusForbidden, "the host removed you from this listening session")
			return
		}
		if ls.guests() >= ls.capacity {
			h.mu.Unlock()
			writeErr(w, http.StatusConflict, "this listening session is full")
			return
		}
		h.leave(u.ID)
		ls.members[u.ID] = &listenMember{user: me, lastSeen: now, online: true}
		ls.order = append(ls.order, u.ID)
		delete(ls.gone, u.ID)
		h.byUser[u.ID] = ls
		h.touch(ls, false)
	}
	snap := ls.snapshot(u.ID, -1, now)
	h.mu.Unlock()
	s.writeListenState(w, r, http.StatusOK, u.ID, snap)
}

// handleListenPoll answers with the session once it's newer than the version
// the device has ("since"), or after listenPollWait with what there is.
func (s *Server) handleListenPoll(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	id := r.PathValue("id")
	since, _ := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)
	known := knownQueueVersion(r)
	h := s.listen
	deadline := time.NewTimer(listenPollWait)
	defer deadline.Stop()
	for {
		h.mu.Lock()
		ls, ok := h.member(w, id, u.ID)
		if !ok {
			h.mu.Unlock()
			return
		}
		now := time.Now()
		ls.members[u.ID].lastSeen = now
		if ls.version > since {
			snap := ls.snapshot(u.ID, known, now)
			h.mu.Unlock()
			s.writeListenState(w, r, http.StatusOK, u.ID, snap)
			return
		}
		changed := ls.changed
		h.mu.Unlock()
		select {
		case <-changed:
		case <-deadline.C:
			since = -1
		case <-r.Context().Done():
			// The device was there while it waited; the host's time away
			// starts from here.
			h.seen(id, u.ID, time.Now())
			return
		}
	}
}

type listenSettingsRequest struct {
	Capacity           *int  `json:"capacity"`
	EveryoneCanControl *bool `json:"everyoneCanControl"`
}

func (s *Server) handleListenSettings(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	var body listenSettingsRequest
	if err := jsonDecodeStrict(r, w, &body); err != nil {
		return
	}
	h := s.listen
	h.mu.Lock()
	ls, ok := h.member(w, r.PathValue("id"), u.ID)
	if !ok {
		h.mu.Unlock()
		return
	}
	if u.ID != ls.hostID {
		h.mu.Unlock()
		writeListenForbidden(w, ls)
		return
	}
	if body.Capacity != nil {
		ls.capacity = clampGuests(*body.Capacity)
	}
	if body.EveryoneCanControl != nil {
		ls.everyoneCanControl = *body.EveryoneCanControl
	}
	h.touch(ls, false)
	snap := ls.snapshot(u.ID, knownQueueVersion(r), time.Now())
	h.mu.Unlock()
	s.writeListenState(w, r, http.StatusOK, u.ID, snap)
}

func (s *Server) handleListenEnd(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	h := s.listen
	h.mu.Lock()
	defer h.mu.Unlock()
	ls, ok := h.member(w, r.PathValue("id"), u.ID)
	if !ok {
		return
	}
	if u.ID != ls.hostID {
		writeListenForbidden(w, ls)
		return
	}
	h.end(ls)
	writeNoContent(w)
}

func (s *Server) handleListenLeave(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	h := s.listen
	h.mu.Lock()
	defer h.mu.Unlock()
	ls := h.sessions[r.PathValue("id")]
	if ls != nil && h.byUser[u.ID] == ls {
		h.leave(u.ID)
	}
	writeNoContent(w)
}

func (s *Server) handleListenKick(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	target := r.PathValue("userId")
	h := s.listen
	h.mu.Lock()
	ls, ok := h.member(w, r.PathValue("id"), u.ID)
	if !ok {
		h.mu.Unlock()
		return
	}
	if u.ID != ls.hostID {
		h.mu.Unlock()
		writeListenForbidden(w, ls)
		return
	}
	if target == ls.hostID {
		h.mu.Unlock()
		writeErr(w, http.StatusBadRequest, "end the session instead")
		return
	}
	if _, in := ls.members[target]; in {
		h.removeMember(ls, target, listenGoneRemoved)
	}
	snap := ls.snapshot(u.ID, knownQueueVersion(r), time.Now())
	h.mu.Unlock()
	s.writeListenState(w, r, http.StatusOK, u.ID, snap)
}

type listenControlRequest struct {
	// seek, next, previous, jump, or ended (a track played to its end on the
	// device; anyone may say so). There's no pause: that's up to each device.
	Action   string   `json:"action"`
	Position *float64 `json:"position"`
	EntryID  string   `json:"entryId"`
}

func (s *Server) handleListenControl(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	var body listenControlRequest
	if err := jsonDecodeStrict(r, w, &body); err != nil {
		return
	}
	h := s.listen
	h.mu.Lock()
	ls, ok := h.member(w, r.PathValue("id"), u.ID)
	if !ok {
		h.mu.Unlock()
		return
	}
	if body.Action != "ended" && !ls.canControl(u.ID) {
		h.mu.Unlock()
		writeListenForbidden(w, ls)
		return
	}
	now := time.Now()
	changed, queueChanged := true, false
	switch body.Action {
	case "seek":
		if body.Position == nil {
			h.mu.Unlock()
			writeErr(w, http.StatusBadRequest, "position is required")
			return
		}
		ls.seek(*body.Position, now)
	case "next":
		queueChanged = ls.advance(now)
	case "previous":
		ls.previous(now)
	case "jump":
		i := ls.indexOf(body.EntryID)
		if i < 0 {
			h.mu.Unlock()
			writeErr(w, http.StatusNotFound, "that song isn't in the queue any more")
			return
		}
		ls.startEntry(i, now)
	case "ended":
		changed, queueChanged = ls.ended(body.EntryID, now)
	default:
		h.mu.Unlock()
		writeErr(w, http.StatusBadRequest, "unknown action")
		return
	}
	if changed {
		h.touch(ls, queueChanged)
	}
	snap := ls.snapshot(u.ID, knownQueueVersion(r), now)
	h.mu.Unlock()
	s.writeListenState(w, r, http.StatusOK, u.ID, snap)
}

type listenAddRequest struct {
	TrackIDs []string `json:"trackIds"`
	// end (the default), next (after the current song), or now (next, and
	// start it).
	Mode string `json:"mode"`
}

func (s *Server) handleListenAdd(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	var body listenAddRequest
	if err := jsonDecodeStrict(r, w, &body); err != nil {
		return
	}
	switch body.Mode {
	case "":
		body.Mode = "end"
	case "end", "next", "now":
	default:
		writeErr(w, http.StatusBadRequest, "unknown mode")
		return
	}
	if len(body.TrackIDs) == 0 || len(body.TrackIDs) > maxListenQueue {
		writeErr(w, http.StatusBadRequest, "trackIds must list 1 to "+strconv.Itoa(maxListenQueue)+" tracks")
		return
	}
	id := r.PathValue("id")
	h := s.listen
	h.mu.Lock()
	ls, ok := h.member(w, id, u.ID)
	if !ok {
		h.mu.Unlock()
		return
	}
	if !ls.canControl(u.ID) {
		h.mu.Unlock()
		writeListenForbidden(w, ls)
		return
	}
	adder := ls.members[u.ID].user
	hostID := ls.hostID
	h.mu.Unlock()

	entries, err := s.listenEntries(adder, hostID, body.TrackIDs)
	if err != nil {
		s.internalErr(w, "load tracks", err)
		return
	}
	if len(entries) == 0 {
		writeErr(w, http.StatusNotFound, "track not found")
		return
	}

	h.mu.Lock()
	ls, ok = h.member(w, id, u.ID)
	if !ok {
		h.mu.Unlock()
		return
	}
	if !ls.canControl(u.ID) {
		h.mu.Unlock()
		writeListenForbidden(w, ls)
		return
	}
	if ls.upcoming()+len(entries) > maxListenQueue {
		h.mu.Unlock()
		writeErr(w, http.StatusConflict, "the queue is full")
		return
	}
	now := time.Now()
	ls.add(entries, body.Mode, now)
	h.touch(ls, true)
	snap := ls.snapshot(u.ID, knownQueueVersion(r), now)
	h.mu.Unlock()
	s.writeListenState(w, r, http.StatusOK, u.ID, snap)
}

func (s *Server) handleListenRemove(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	h := s.listen
	h.mu.Lock()
	ls, ok := h.member(w, r.PathValue("id"), u.ID)
	if !ok {
		h.mu.Unlock()
		return
	}
	i := ls.indexOf(r.PathValue("entryId"))
	switch {
	case i < 0:
		h.mu.Unlock()
		writeErr(w, http.StatusNotFound, "that song isn't in the queue any more")
		return
	case i == ls.current:
		h.mu.Unlock()
		writeErr(w, http.StatusConflict, "that song is playing now")
		return
	case !ls.canControl(u.ID) && ls.entries[i].addedBy.ID != u.ID:
		h.mu.Unlock()
		writeListenForbidden(w, ls)
		return
	}
	ls.entries = slices.Delete(ls.entries, i, i+1)
	if i < ls.current {
		ls.current--
	}
	h.touch(ls, true)
	snap := ls.snapshot(u.ID, knownQueueVersion(r), time.Now())
	h.mu.Unlock()
	s.writeListenState(w, r, http.StatusOK, u.ID, snap)
}

// handleListenClear empties what's queued after the current song, which plays
// on. It's the host's alone, even when everyone may add songs and skip.
func (s *Server) handleListenClear(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	h := s.listen
	h.mu.Lock()
	ls, ok := h.member(w, r.PathValue("id"), u.ID)
	if !ok {
		h.mu.Unlock()
		return
	}
	if u.ID != ls.hostID {
		h.mu.Unlock()
		writeListenForbidden(w, ls)
		return
	}
	if ls.upcoming() > 0 {
		ls.entries = ls.entries[:ls.current+1]
		h.touch(ls, true)
	}
	snap := ls.snapshot(u.ID, knownQueueVersion(r), time.Now())
	h.mu.Unlock()
	s.writeListenState(w, r, http.StatusOK, u.ID, snap)
}
