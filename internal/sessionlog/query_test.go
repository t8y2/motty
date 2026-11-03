package sessionlog

import (
	"fmt"
	"slices"
	"testing"
	"time"
)

func TestDurationSortPutsOngoingLast(t *testing.T) {
	s, _ := openTestStore(t)
	base := testBase().Add(-time.Hour)
	for _, fixture := range []struct {
		id              string
		start, duration time.Duration
	}{
		{"a", 0, 2 * time.Minute}, {"b", 0, 2 * time.Minute},
		{"c", time.Minute, time.Minute}, {"d", 2 * time.Minute, 0},
		{"e", 3 * time.Minute, 2 * time.Minute},
		{"f", 2 * time.Minute, 0}, {"g", 4 * time.Minute, 0},
	} {
		r := sampleRecord(fixture.id, base.Add(fixture.start))
		startRecord(t, s, r)
		if fixture.duration != 0 {
			if err := s.Finish(r.ID, StatusEnded, r.StartedAt.Add(fixture.duration), ReasonUserClosed); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, tc := range []struct {
		order SortOrder
		want  []string
	}{
		{SortStartedDesc, []string{"g", "e", "f", "d", "c", "b", "a"}},
		{SortStartedAsc, []string{"a", "b", "c", "d", "f", "e", "g"}},
		{SortDurationDesc, []string{"e", "b", "a", "c", "g", "f", "d"}},
		{SortDurationAsc, []string{"c", "e", "b", "a", "g", "f", "d"}},
	} {
		query := Query{Sort: tc.order, Limit: 2}
		var ids []string
		for range 4 {
			page := mustList(t, s, query)
			if page.Total != 7 || page.Matched != 7 {
				t.Fatalf("counts changed with cursor: %+v", page)
			}
			for _, r := range page.Records {
				ids = append(ids, r.ID)
			}
			if !page.More {
				break
			}
			if page.Next.Sort != tc.order {
				t.Fatal("cursor lost its sort identity")
			}
			query.Cursor = page.Next
		}
		if !slices.Equal(ids, tc.want) {
			t.Fatalf("sort %d: got %v, want %v", tc.order, ids, tc.want)
		}
	}
	page := mustList(t, s, Query{Search: "no matching host"})
	if page.Total != 7 || page.Matched != 0 || len(page.Records) != 0 {
		t.Fatalf("zero-match counts = %+v", page)
	}
}

func TestListSortAndCountsAcrossPages(t *testing.T) {
	s, _ := openTestStore(t)
	base := testBase().Add(-time.Hour)
	for i := range 123 {
		r := sampleRecord(fmt.Sprintf("row-%03d", i), base.Add(time.Duration(i)*time.Millisecond))
		r.Favorite = i%2 == 0
		startRecord(t, s, r)
		if err := s.Finish(r.ID, StatusEnded, r.StartedAt.Add(time.Second), ReasonUserClosed); err != nil {
			t.Fatal(err)
		}
	}
	all := mustList(t, s, Query{})
	if len(all.Records) != 50 || all.Matched != 123 || all.Total != 123 {
		t.Fatalf("first page counts = %+v", all)
	}
	favorite := true
	query := Query{Favorite: &favorite}
	first := mustList(t, s, query)
	if len(first.Records) != 50 || first.Matched != 62 || first.Total != 123 || first.Next.ID != "row-024" {
		t.Fatalf("favorite first page = %+v", first)
	}
	newer := sampleRecord("newer", base.Add(time.Hour))
	newer.Favorite = true
	startRecord(t, s, newer)
	query.Cursor = first.Next
	second := mustList(t, s, query)
	if len(second.Records) != 12 || second.Matched != 63 || second.Total != 124 || second.More {
		t.Fatalf("second page counts = %+v", second)
	}
	if second.Records[0].ID != "row-022" || second.Records[11].ID != "row-000" {
		t.Fatalf("new insert shifted the page: %+v", second.Records)
	}
}

func TestSortCursorRejectsMismatchedOrder(t *testing.T) {
	s, _ := openTestStore(t)
	for _, query := range []Query{
		{Sort: SortOrder(255)},
		{Sort: SortDurationDesc, Cursor: Cursor{StartedAt: testBase(), ID: "some-id", Sort: SortStartedDesc}},
		{Cursor: Cursor{StartedAt: testBase()}},
		{Cursor: Cursor{ID: "some-id"}},
	} {
		if _, err := s.List(query); err == nil {
			t.Fatalf("invalid sort/cursor accepted: %+v", query)
		}
	}
}
