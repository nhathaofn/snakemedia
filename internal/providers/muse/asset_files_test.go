package muse

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func obstructMuseJournal(t *testing.T, path string) func() {
	t.Helper()
	backup := path + ".test-backup"
	if err := os.Rename(path, backup); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	return func() {
		t.Helper()
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(backup, path); err != nil {
			t.Fatal(err)
		}
	}
}

func TestVaultEditRollsBackFilesAndIndexOnFailure(t *testing.T) {
	p := testProvider(t, &fakePage{})
	original := testPNGBytes(t, 24, 24)
	url, err := p.media.Put(assetPrefix+"rollback", "image/png", ".png", original)
	if err != nil {
		t.Fatal(err)
	}
	res := p.SaveAsset(MuseAsset{
		Tag: "original", Name: "Original", Type: MuseTypeProp, Role: MuseRoleProp,
		Items: []MuseAssetItem{{URL: url, Primary: true}},
	})
	if !res.Success {
		t.Fatal(res.Error)
	}
	asset := res.Data
	previous, err := p.media.VaultPath(asset.Items[0].URL)
	if err != nil {
		t.Fatal(err)
	}
	assertOriginal := func() {
		t.Helper()
		got, _ := p.assets.Get(asset.ID)
		if got.Tag != "original" || got.Items[0].Angle != "" || got.Items[0].URL != asset.Items[0].URL {
			t.Fatalf("old index changed after failure: %+v", got)
		}
		body, err := os.ReadFile(previous)
		if err != nil || !bytes.Equal(body, original) {
			t.Fatalf("old PNG unavailable or changed: %v", err)
		}
		entries, err := os.ReadDir(filepath.Dir(previous))
		if err != nil || len(entries) != 1 {
			t.Fatalf("rollback left extra files: %v (%v)", entries, err)
		}
	}

	edit := asset
	edit.Tag = "renamed"
	edit.Items = append(edit.Items, MuseAssetItem{URL: "/flow-media/missing-test.png"})
	if res := p.SaveAsset(edit); res.Success {
		t.Fatal("expected a missing image error")
	}
	assertOriginal()

	restore := obstructMuseJournal(t, p.assets.Path())
	angle := "front"
	res = p.UpdateAssetItem(asset.ID, asset.Items[0].ID, MuseAssetItemPatch{Angle: &angle})
	restore()
	if res.Success {
		t.Fatal("expected an asset index write error")
	}
	assertOriginal()

	res = p.UpdateAssetItem(asset.ID, asset.Items[0].ID, MuseAssetItemPatch{Angle: &angle})
	if !res.Success {
		t.Fatal(res.Error)
	}
	current, err := p.media.VaultPath(res.Data.Items[0].URL)
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(current)
	if err != nil || !bytes.Equal(body, original) {
		t.Fatalf("updated file invalid: %v", err)
	}
	if _, err := os.Stat(previous); !os.IsNotExist(err) {
		t.Fatalf("old file still present after commit: %v", err)
	}
}

func TestMuseSaveRetryAfterJournalFailureKeepsOneCopy(t *testing.T) {
	p := testProvider(t, &fakePage{})
	url, err := p.media.Put("muse-save-once", "image/png", ".png", testPNGBytes(t, 24, 24))
	if err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	p.media.AllowRoot(out)
	job := MuseJob{
		ID: "save-once", Kind: MuseJobImage, Mode: MuseJobBatch, Status: MuseJobSucceeded,
		Prompt: "test", OutputDir: out, Index: 1, Files: []string{url},
	}
	if _, err := p.jobs.add(job); err != nil {
		t.Fatal(err)
	}
	restore := obstructMuseJournal(t, p.jobs.journal.Path())
	res := p.RetrySave(job.ID)
	restore()
	if res.Success {
		t.Fatal("expected a journal write failure after copying")
	}
	before, err := os.ReadDir(out)
	if err != nil || len(before) != 1 {
		t.Fatalf("expected one published file: %v (%v)", before, err)
	}
	if err := p.jobs.load(); err != nil {
		t.Fatal(err)
	}
	res = p.RetrySave(job.ID)
	if !res.Success || len(res.Data.SavedFiles) != 1 || res.Data.SaveError != "" {
		t.Fatalf("save retry failed: %+v", res)
	}
	after, err := os.ReadDir(out)
	if err != nil || len(after) != 1 || after[0].Name() != before[0].Name() {
		t.Fatalf("retry should keep the same file: before=%v after=%v (%v)", before, after, err)
	}
}
