package server

import "testing"

func TestMergeTrackAddsUpPlays(t *testing.T) {
	_, s := newTestClient(t)
	userID := seedLibrary(t, s)
	for id, plays := range map[string]int{"shape": 3, "perfect": 2} {
		if _, err := s.db.Exec(`INSERT INTO play_stats (user_id, track_id, plays, last_played_at) VALUES (?,?,?,?)`,
			userID, id, plays, nowRFC3339()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.db.Exec(`INSERT INTO favorites (user_id, track_id, created_at) VALUES (?,?,?)`, userID, "shape", nowRFC3339()); err != nil {
		t.Fatal(err)
	}

	if err := s.mergeTrackInto("shape", "perfect"); err != nil {
		t.Fatalf("merge: %v", err)
	}

	var plays, favorite int
	if err := s.db.QueryRow(`SELECT plays FROM play_stats WHERE user_id = ? AND track_id = 'perfect'`, userID).Scan(&plays); err != nil {
		t.Fatal(err)
	}
	if plays != 5 {
		t.Fatalf("plays after merge: %d, want 5", plays)
	}
	if err := s.db.QueryRow(`SELECT COUNT(1) FROM favorites WHERE user_id = ? AND track_id = 'perfect'`, userID).Scan(&favorite); err != nil {
		t.Fatal(err)
	}
	if favorite != 1 {
		t.Fatal("the favorite did not move to the kept track")
	}
}
