package diff

import (
	"testing"

	"github.com/kerem/unbound-dns/internal/records"
)

func rec(t *testing.T, name string, typ records.Type, value string) records.Record {
	t.Helper()

	r, err := records.New(name, typ, value, nil)
	if err != nil {
		t.Fatalf("kayıt oluşturulamadı: %v", err)
	}

	return r
}

func TestCompareDetectsIdenticalSets(t *testing.T) {
	a := rec(t, "mail.example.com", records.TypeA, "10.0.0.1")

	result := Compare([]ServerSet{
		{Label: "ns1", Records: []records.Record{a}},
		{Label: "ns2", Records: []records.Record{a}},
	})

	if !result.InSync() {
		t.Errorf("aynı kayıt kümeleri farklı sayıldı: %+v", result.Entries)
	}
	if result.Entries[0].Status != StatusSame {
		t.Errorf("durum = %s", result.Entries[0].Status)
	}
}

func TestCompareDetectsMissingRecord(t *testing.T) {
	result := Compare([]ServerSet{
		{Label: "ns1", Records: []records.Record{
			rec(t, "mail.example.com", records.TypeA, "10.0.0.1"),
			rec(t, "web.example.com", records.TypeA, "10.0.0.2"),
		}},
		{Label: "ns2", Records: []records.Record{
			rec(t, "mail.example.com", records.TypeA, "10.0.0.1"),
		}},
	})

	missing := result.Missing()
	if len(missing) != 1 {
		t.Fatalf("%d eksik kayıt bulundu, beklenen 1", len(missing))
	}

	entry := missing[0]
	if entry.Record.Name != "web.example.com." {
		t.Errorf("eksik kayıt = %q", entry.Record.Name)
	}
	if len(entry.Missing) != 1 || entry.Missing[0] != "ns2" {
		t.Errorf("eksik sunucular = %v", entry.Missing)
	}
	if len(entry.Present) != 1 || entry.Present[0] != "ns1" {
		t.Errorf("var olan sunucular = %v", entry.Present)
	}
}

func TestCompareDetectsConflict(t *testing.T) {
	result := Compare([]ServerSet{
		{Label: "ns1", Records: []records.Record{rec(t, "mail.example.com", records.TypeA, "10.0.0.1")}},
		{Label: "ns2", Records: []records.Record{rec(t, "mail.example.com", records.TypeA, "10.9.9.9")}},
	})

	conflicts := result.Conflicts()
	if len(conflicts) != 1 {
		t.Fatalf("%d çakışma bulundu, beklenen 1", len(conflicts))
	}

	values := conflicts[0].Values
	if values["ns1"] != "10.0.0.1" || values["ns2"] != "10.9.9.9" {
		t.Errorf("değerler = %v", values)
	}
}

// A record present on both servers with the same name and type but different
// values is a conflict, not a missing record; sync must not choose for the
// user.
func TestConflictOutranksMissing(t *testing.T) {
	result := Compare([]ServerSet{
		{Label: "ns1", Records: []records.Record{rec(t, "h.example.com", records.TypeA, "10.0.0.1")}},
		{Label: "ns2", Records: []records.Record{rec(t, "h.example.com", records.TypeA, "10.0.0.2")}},
		{Label: "ns3", Records: []records.Record{}},
	})

	if got := result.Entries[0].Status; got != StatusConflict {
		t.Errorf("durum = %s, çakışma beklenirdi", got)
	}
}

// A differing TTL does not change what the resolver answers, so it must not
// register as a difference.
func TestCompareIgnoresTTL(t *testing.T) {
	withTTL, _ := records.New("h.example.com", records.TypeA, "10.0.0.1", new(uint32(300)))
	withoutTTL := rec(t, "h.example.com", records.TypeA, "10.0.0.1")

	result := Compare([]ServerSet{
		{Label: "ns1", Records: []records.Record{withTTL}},
		{Label: "ns2", Records: []records.Record{withoutTTL}},
	})

	if !result.InSync() {
		t.Errorf("yalnızca TTL farkı fark sayıldı: %+v", result.Entries)
	}
}

// The same name with different types is two records, not a conflict.
func TestCompareSeparatesTypes(t *testing.T) {
	result := Compare([]ServerSet{
		{Label: "ns1", Records: []records.Record{
			rec(t, "h.example.com", records.TypeA, "10.0.0.1"),
			rec(t, "h.example.com", records.TypeAAAA, "2001:db8::1"),
		}},
		{Label: "ns2", Records: []records.Record{
			rec(t, "h.example.com", records.TypeA, "10.0.0.1"),
			rec(t, "h.example.com", records.TypeAAAA, "2001:db8::1"),
		}},
	})

	if len(result.Entries) != 2 {
		t.Fatalf("%d kayıt karşılaştırıldı, beklenen 2", len(result.Entries))
	}
	if !result.InSync() {
		t.Error("aynı kümeler farklı sayıldı")
	}
}

func TestPlanSyncAddsMissingRecords(t *testing.T) {
	source := ServerSet{Label: "ns1", Records: []records.Record{
		rec(t, "mail.example.com", records.TypeA, "10.0.0.1"),
		rec(t, "web.example.com", records.TypeA, "10.0.0.2"),
	}}
	target := ServerSet{Label: "ns2", Records: []records.Record{
		rec(t, "mail.example.com", records.TypeA, "10.0.0.1"),
	}}

	plans, conflicts := PlanSync(source, []ServerSet{target}, false)

	if len(conflicts) != 0 {
		t.Fatalf("çakışma bildirildi: %+v", conflicts)
	}
	if len(plans) != 1 {
		t.Fatalf("%d plan üretildi", len(plans))
	}
	if len(plans[0].Add) != 1 || plans[0].Add[0].Name != "web.example.com." {
		t.Errorf("eklenecekler = %+v", plans[0].Add)
	}
	if len(plans[0].Remove) != 0 {
		t.Errorf("prune istenmediği halde silme planlandı: %+v", plans[0].Remove)
	}
}

// Without --prune, a record the target has and the source does not must stay.
func TestPlanSyncKeepsExtraRecordsWithoutPrune(t *testing.T) {
	source := ServerSet{Label: "ns1", Records: []records.Record{
		rec(t, "mail.example.com", records.TypeA, "10.0.0.1"),
	}}
	target := ServerSet{Label: "ns2", Records: []records.Record{
		rec(t, "mail.example.com", records.TypeA, "10.0.0.1"),
		rec(t, "fazla.example.com", records.TypeA, "10.0.0.5"),
	}}

	plans, _ := PlanSync(source, []ServerSet{target}, false)

	if !plans[0].Empty() {
		t.Errorf("prune yokken değişiklik planlandı: %+v", plans[0])
	}
}

func TestPlanSyncPrunesWhenAsked(t *testing.T) {
	source := ServerSet{Label: "ns1", Records: []records.Record{
		rec(t, "mail.example.com", records.TypeA, "10.0.0.1"),
	}}
	target := ServerSet{Label: "ns2", Records: []records.Record{
		rec(t, "mail.example.com", records.TypeA, "10.0.0.1"),
		rec(t, "fazla.example.com", records.TypeA, "10.0.0.5"),
	}}

	plans, _ := PlanSync(source, []ServerSet{target}, true)

	if len(plans[0].Remove) != 1 || plans[0].Remove[0].Name != "fazla.example.com." {
		t.Errorf("silinecekler = %+v", plans[0].Remove)
	}
}

// Choosing between two values is the user's call, so a conflicting record is
// left out of the plan entirely and reported instead.
func TestPlanSyncSkipsConflicts(t *testing.T) {
	source := ServerSet{Label: "ns1", Records: []records.Record{
		rec(t, "mail.example.com", records.TypeA, "10.0.0.1"),
		rec(t, "web.example.com", records.TypeA, "10.0.0.2"),
	}}
	target := ServerSet{Label: "ns2", Records: []records.Record{
		rec(t, "mail.example.com", records.TypeA, "10.9.9.9"),
	}}

	plans, conflicts := PlanSync(source, []ServerSet{target}, true)

	if len(conflicts) != 1 {
		t.Fatalf("%d çakışma bildirildi, beklenen 1", len(conflicts))
	}
	if conflicts[0].Record.Name != "mail.example.com." {
		t.Errorf("çakışan kayıt = %q", conflicts[0].Record.Name)
	}

	for _, rec := range plans[0].Add {
		if rec.Name == "mail.example.com." {
			t.Error("çakışan kayıt eklenmek üzere planlandı")
		}
	}
	for _, rec := range plans[0].Remove {
		if rec.Name == "mail.example.com." {
			t.Error("çakışan kayıt silinmek üzere planlandı")
		}
	}

	// The non-conflicting record still gets synced.
	if len(plans[0].Add) != 1 || plans[0].Add[0].Name != "web.example.com." {
		t.Errorf("çakışmayan kayıt eklenmedi: %+v", plans[0].Add)
	}
}

func TestCompareIsDeterministic(t *testing.T) {
	sets := []ServerSet{
		{Label: "ns1", Records: []records.Record{
			rec(t, "c.example.com", records.TypeA, "10.0.0.3"),
			rec(t, "a.example.com", records.TypeA, "10.0.0.1"),
			rec(t, "b.example.com", records.TypeA, "10.0.0.2"),
		}},
		{Label: "ns2", Records: []records.Record{}},
	}

	first := Compare(sets)

	for range 5 {
		again := Compare(sets)

		for i := range first.Entries {
			if first.Entries[i].Record.Name != again.Entries[i].Record.Name {
				t.Fatalf("sıralama kararsız: %q vs %q",
					first.Entries[i].Record.Name, again.Entries[i].Record.Name)
			}
		}
	}
}
