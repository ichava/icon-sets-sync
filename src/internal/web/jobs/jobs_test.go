package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func clock() func() time.Time {
	var n atomic.Int64
	base := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	return func() time.Time { return base.Add(time.Duration(n.Add(1)) * time.Second) }
}

func wait(t *testing.T, j *Job) {
	t.Helper()
	select {
	case <-j.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("job did not finish")
	}
}

func TestEventsArriveInOrderWithRisingIDs(t *testing.T) {
	r := NewRegistry(context.Background(), nil, clock())
	j, err := r.Start(Spec{Kind: KindSync, Packs: []string{"a"}}, func(_ context.Context, j *Job) (Status, error) {
		j.PackStarted("a", "1 -> 2")
		log := slog.New(Handler(j, slog.LevelInfo))
		log.Debug("hidden")
		log.Info("stage", "pipeline", "a@2", "stage", "NpmTarball")
		Observer(j).StageStarted("a@2", "NpmTarball")
		Observer(j).StageFinished("a@2", "NpmTarball", map[string]any{"npm": map[string]any{"files": 3}}, nil)
		Observer(j).StageStarted("a@2", "Filesystem")
		Observer(j).StageFinished("a@2", "Filesystem", nil, errors.New("disk full"))
		j.PackDone("a", "failed", "disk full", "")
		return StatusFailed, errors.New("1 pack failed")
	})
	if err != nil {
		t.Fatal(err)
	}
	wait(t, j)
	var kinds []string
	for i, e := range j.Events() {
		if e.ID != int64(i+1) {
			t.Fatalf("event %d has ID %d", i, e.ID)
		}
		k := e.Type
		if e.State != "" {
			k += ":" + e.State
		}
		kinds = append(kinds, k)
	}
	want := "pack-start log stage:started stage:finished stage:started stage:failed pack-done done"
	if got := strings.Join(kinds, " "); got != want {
		t.Fatalf("events\n got %s\nwant %s", got, want)
	}
	evs := j.Events()
	if evs[1].Attrs["stage"] != "NpmTarball" || evs[1].Level != "info" {
		t.Errorf("log event %+v", evs[1])
	}
	if evs[5].Err != "disk full" {
		t.Errorf("failed stage %+v", evs[5])
	}
	if info := j.Info(); info.Status != StatusFailed || info.Err != "1 pack failed" || info.Ended.IsZero() {
		t.Errorf("info %+v", info)
	}
}

func TestSinceReplaysExactlyAfterTheLastEventID(t *testing.T) {
	r := NewRegistry(context.Background(), nil, clock())
	j, _ := r.Start(Spec{Kind: KindSync}, func(_ context.Context, j *Job) (Status, error) {
		for i := range 5 {
			j.Logf("info", "line %d", i)
		}
		return StatusSucceeded, nil
	})
	wait(t, j)
	evs, done, _ := j.Since(3)
	if !done || len(evs) != 3 || evs[0].ID != 4 || evs[2].Type != EventDone {
		t.Fatalf("since(3) = %+v done=%v", evs, done)
	}
}

func TestTheRingKeepsTheNewestEvents(t *testing.T) {
	r := NewRegistry(context.Background(), nil, clock())
	r.SetCapacity(4)
	j, _ := r.Start(Spec{Kind: KindSync}, func(_ context.Context, j *Job) (Status, error) {
		for i := range 10 {
			j.Logf("info", "line %d", i)
		}
		return StatusSucceeded, nil
	})
	wait(t, j)
	evs := j.Events()
	if len(evs) != 4 || evs[len(evs)-1].Type != EventDone || evs[0].ID != 8 {
		t.Fatalf("retained %+v", evs)
	}
}

func TestCancelEndsTheJobAsCancelled(t *testing.T) {
	r := NewRegistry(context.Background(), nil, clock())
	started := make(chan struct{})
	j, _ := r.Start(Spec{Kind: KindSync, Packs: []string{"a"}}, func(ctx context.Context, j *Job) (Status, error) {
		close(started)
		<-ctx.Done()
		return StatusFailed, ctx.Err()
	})
	<-started
	if !r.Cancel(j.Info().ID) {
		t.Fatal("cancel did not find the job")
	}
	wait(t, j)
	if s := j.Info().Status; s != StatusCancelled {
		t.Fatalf("status %s", s)
	}
	evs := j.Events()
	if last := evs[len(evs)-1]; last.Type != EventDone || last.Status != string(StatusCancelled) {
		t.Errorf("last event %+v", last)
	}
	// Nothing lands after the done event.
	j.Logf("info", "late")
	if n := len(j.Events()); n != len(evs) {
		t.Error("an event was appended after done")
	}
}

func TestOneJobPerPack(t *testing.T) {
	r := NewRegistry(context.Background(), nil, clock())
	release := make(chan struct{})
	first, err := r.Start(Spec{Kind: KindSync, Packs: []string{"a"}}, func(context.Context, *Job) (Status, error) {
		<-release
		return StatusSucceeded, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.Start(Spec{Kind: KindSyncAll, Packs: []string{"b", "a"}}, func(context.Context, *Job) (Status, error) {
		return StatusSucceeded, nil
	})
	var busy *ErrBusy
	if !errors.As(err, &busy) || busy.Pack != "a" || busy.JobID != first.Info().ID {
		t.Fatalf("err %v", err)
	}
	if r.Running("b") != "" {
		t.Error("a refused start kept the lock on b")
	}
	close(release)
	wait(t, first)
	if r.Running("a") != "" {
		t.Error("a finished job kept its lock")
	}
	if _, err := r.Start(Spec{Kind: KindSync, Packs: []string{"a"}}, func(context.Context, *Job) (Status, error) {
		return StatusSucceeded, nil
	}); err != nil {
		t.Errorf("the lock was not released: %v", err)
	}
	r.Wait()
}

func TestAPanickingRunFailsTheJobNotTheServer(t *testing.T) {
	r := NewRegistry(context.Background(), nil, clock())
	j, _ := r.Start(Spec{Kind: KindSync, Packs: []string{"a"}}, func(context.Context, *Job) (Status, error) {
		panic("boom")
	})
	wait(t, j)
	if info := j.Info(); info.Status != StatusFailed || !strings.Contains(info.Err, "boom") || r.Running("a") != "" {
		t.Fatalf("info %+v", info)
	}
}

func TestStorePersistsAndPrunesToTheCeiling(t *testing.T) {
	if DefaultKeep != 50 {
		t.Fatalf("DefaultKeep is %d; the history ceiling is 50 by design", DefaultKeep)
	}
	dir := t.TempDir()
	// A file the store did not write must survive pruning.
	foreign := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(foreign, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Store{Dir: dir, Keep: 3}
	r := NewRegistry(context.Background(), s, clock())
	var ids []string
	for i := range 5 {
		j, err := r.Start(Spec{Kind: KindSync, Packs: []string{fmt.Sprint(i)}, Label: fmt.Sprint(i)},
			func(_ context.Context, j *Job) (Status, error) {
				j.Logf("info", "hello %d", i)
				return StatusSucceeded, nil
			})
		if err != nil {
			t.Fatal(err)
		}
		wait(t, j)
		ids = append(ids, j.Info().ID)
	}
	stored, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 3 || stored[0].ID != ids[4] || stored[2].ID != ids[2] {
		t.Fatalf("kept %+v", stored)
	}
	if _, err := os.Stat(foreign); err != nil {
		t.Error("pruning removed a file it did not write")
	}
	info, events, err := s.Load(ids[4])
	if err != nil || info.Status != StatusSucceeded || len(events) != 2 || events[0].Msg != "hello 4" {
		t.Fatalf("load %+v %+v %v", info, events, err)
	}
	if _, _, err := s.Load(ids[0]); !errors.Is(err, ErrNotFound) {
		t.Errorf("a pruned job loaded: %v", err)
	}
	// The registry lists this process's jobs and the history, each once.
	if n := len(r.List()); n != 5 {
		t.Errorf("listed %d jobs", n)
	}
}

func TestStoreRefusesAPathForAnID(t *testing.T) {
	parent := t.TempDir()
	// A file just outside the store that a traversing ID would reach.
	if err := os.WriteFile(filepath.Join(parent, "secret.jsonl"), []byte(`{"id":"secret"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := &Store{Dir: filepath.Join(parent, "runs")}
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"../secret", "../../etc/passwd", "20261023T120000Z-abcdef/../x", "x"} {
		if _, err := s.Open(id); !errors.Is(err, ErrNotFound) {
			t.Errorf("Open(%q) = %v", id, err)
		}
	}
	if !ValidID(newID(time.Now())) {
		t.Error("newID does not produce a valid ID")
	}
}
