package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNormalizeUpgradeFixture(t *testing.T) {
	dump := "CREATE TABLE `schema_migration` (\n  `id` varchar(191) NOT NULL\n) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;\n" +
		"CREATE TABLE `agent` (\n  `id` bigint unsigned NOT NULL AUTO_INCREMENT\n) ENGINE=InnoDB AUTO_INCREMENT=7 DEFAULT CHARSET=utf8mb4;\n" +
		"INSERT INTO `schema_migration` VALUES ('system_grant_live_marker','2026-09-26 11:45:36.200989');\n"
	manifest := upgradeFixtureManifest{SourceImage: upgradeFixtureImage + ":0.2.0-alpha.14", AgentID: "i34elblrzj6ws2miy3la"}
	out, err := normalizeUpgradeFixture("0.2.0-alpha.14", manifest, dump)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	if strings.Contains(got, "AUTO_INCREMENT=") {
		t.Errorf("the table counter survived normalization:\n%s", got)
	}
	if !strings.Contains(got, "`id` bigint unsigned NOT NULL AUTO_INCREMENT\n") {
		t.Errorf("normalization removed the column attribute, not only the counter:\n%s", got)
	}
	if !strings.Contains(got, "upgrade-fixture 0.2.0-alpha.14") {
		t.Errorf("the header does not name the command that regenerates it:\n%s", got)
	}
	var line string
	for _, l := range strings.Split(got, "\n") {
		if strings.HasPrefix(l, upgradeFixtureManifestPrefix) {
			line = strings.TrimPrefix(l, upgradeFixtureManifestPrefix)
		}
	}
	var back upgradeFixtureManifest
	if err := json.Unmarshal([]byte(line), &back); err != nil || back != manifest {
		t.Errorf("manifest line %q decodes to %+v (%v), want %+v", line, back, err, manifest)
	}

	if _, err := normalizeUpgradeFixture("0.2.0-alpha.1", manifest, "CREATE TABLE `agent` (\n);\n"); err == nil {
		t.Error("a dump without the migration ledger was accepted; the upgrade test could not tell what it had applied")
	}
}

func TestUpgradeFixtureRejectsABadTag(t *testing.T) {
	for _, args := range [][]string{nil, {"latest"}, {"0.2.0-alpha.14", "extra"}, {"../0.2.0"}} {
		if err := cmdReleaseUpgradeFixture(args); err == nil {
			t.Errorf("release upgrade-fixture %v was accepted", args)
		}
	}
	if !upgradeFixtureTag.MatchString(strings.TrimPrefix("v0.2.0-alpha.14", "v")) {
		t.Error("a release tag with its v prefix should name the image tag without it")
	}
}
