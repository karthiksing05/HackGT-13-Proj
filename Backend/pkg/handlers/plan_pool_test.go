package handlers

import (
	"Backend/pkg/itinerary"
	"Backend/pkg/models"
	"testing"
)

func TestPlanPoolPaging(t *testing.T) {
	all := make([]models.PlanOption, 7)
	for i := range all {
		all[i].ID = string(rune('a' + i))
	}
	cursor := savePlanPool(itinerary.Window{Pace: "packed"}, all[3:], all)
	if cursor == "" {
		t.Fatal("expected a cursor for the remaining options")
	}
	var got []string
	for cursor != "" {
		page, next, ok := nextFromPlanPool(cursor, morePageOptions)
		if !ok {
			t.Fatalf("cursor %q not recognised", cursor)
		}
		for _, o := range page {
			got = append(got, o.ID)
		}
		cursor = next
	}
	if len(got) != 4 || got[0] != "d" || got[3] != "g" {
		t.Errorf("pages = %v", got)
	}
	if w, ok := planWindowFor("b"); !ok || w.Pace != "packed" {
		t.Error("each option should remember its window")
	}
	if _, _, ok := nextFromPlanPool("legacy-cursor", 2); ok {
		t.Error("a legacy cursor should not be treated as a pool cursor")
	}
	if page, next, ok := nextFromPlanPool("dag_expired", 2); !ok || page != nil || next != "" {
		t.Error("an unknown DAG cursor should return an empty last page")
	}
}
