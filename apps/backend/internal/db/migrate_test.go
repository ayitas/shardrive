package db

import (
	"testing"
	"testing/fstest"
)

func TestLoadMigrationsSortsVersionsAndIgnoresDownFiles(t *testing.T) {
	loaded, err := loadMigrations(fstest.MapFS{
		"000010_ten.up.sql":   {Data: []byte("SELECT 10")},
		"000002_two.up.sql":   {Data: []byte("SELECT 2")},
		"000002_two.down.sql": {Data: []byte("SELECT -2")},
	})
	if err != nil {
		t.Fatalf("loadMigrations() error = %v", err)
	}
	if len(loaded) != 2 || loaded[0].version != 2 || loaded[1].version != 10 {
		t.Fatalf("unexpected migrations: %+v", loaded)
	}
}

func TestLoadMigrationsRejectsDuplicateVersions(t *testing.T) {
	_, err := loadMigrations(fstest.MapFS{
		"000001_first.up.sql":  {Data: []byte("SELECT 1")},
		"000001_second.up.sql": {Data: []byte("SELECT 2")},
	})
	if err == nil {
		t.Fatal("loadMigrations() error = nil, want duplicate version error")
	}
}
