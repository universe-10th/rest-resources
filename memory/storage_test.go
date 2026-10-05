package memory

import (
	"fmt"
	"testing"

	"github.com/universe-10th/rest-resources/types"
)

type memoryTestResource struct {
	Resource[int]
	Name   string `json:"name"`
	Rank   int    `json:"rank"`
	Parent int    `json:"parent_id"`
}

type memorySoftTestResource struct {
	SoftDeletedResource[int]
	Name string `json:"name"`
}

type memoryStringIDResource struct {
	Resource[string]
	Name string `json:"name"`
}

func TestStorageSavesUpdatesFiltersSortsAndPages(t *testing.T) {
	t.Parallel()

	storage := NewStorage[int, *memoryTestResource]()
	first := &memoryTestResource{Name: "alpha", Rank: 2, Parent: 10}
	second := &memoryTestResource{Name: "beta", Rank: 1, Parent: 10}
	third := &memoryTestResource{Name: "alphabet", Rank: 3, Parent: 20}

	for _, element := range []*memoryTestResource{first, second, third} {
		if notFound, err := storage.Save(element); err != nil || notFound {
			t.Fatalf("Save returned notFound=%v err=%v", notFound, err)
		}
		if element.ID == 0 {
			t.Fatal("expected generated ID")
		}
	}

	first.Name = "alpha-updated"
	first.Rank = 4
	if notFound, err := storage.Save(first); err != nil || notFound {
		t.Fatalf("update Save returned notFound=%v err=%v", notFound, err)
	}

	filter := &types.FilterExpression{
		Operator: types.FilterAnd,
		Expressions: []types.FilterExpression{
			{Operator: types.FilterEQ, Field: "parent_id", Value: 10},
			{Operator: types.FilterContains, Field: "name", Value: "a"},
		},
	}
	sortExpression := &types.SortExpression{
		Sort: []types.Sort{{Field: "rank", Order: types.Asc}},
	}
	elements, total, err := storage.GetElements(filter, sortExpression, 0, 1)
	if err != nil {
		t.Fatalf("GetElements returned error: %v", err)
	}
	if total != 2 {
		t.Fatalf("expected total 2, got %d", total)
	}
	if len(elements) != 1 || elements[0].ID != second.ID {
		t.Fatalf("expected first page to contain second element, got %#v", elements)
	}

	idFilter := types.FilterExpression{}
	storage.AddIDFilter(&idFilter, first.ID)
	found, ok, err := storage.GetElement(&idFilter)
	if err != nil || !ok {
		t.Fatalf("GetElement returned found=%v err=%v", ok, err)
	}
	if found.Name != "alpha-updated" || found.CreatedAt.IsZero() || found.UpdatedAt.IsZero() {
		t.Fatalf("unexpected updated element: %#v", found)
	}
}

func TestStorageSoftDeleteRestoreAndPrune(t *testing.T) {
	t.Parallel()

	storage := NewStorage[int, *memorySoftTestResource]()
	element := &memorySoftTestResource{Name: "soft"}
	if notFound, err := storage.Save(element); err != nil || notFound {
		t.Fatalf("Save returned notFound=%v err=%v", notFound, err)
	}

	if notFound, err := storage.Delete(element); err != nil || notFound {
		t.Fatalf("Delete returned notFound=%v err=%v", notFound, err)
	}

	activeFilter := types.FilterExpression{}
	storage.AddIDFilter(&activeFilter, element.ID)
	storage.AddDeletedFilter(&activeFilter, false)
	if _, found, err := storage.GetElement(&activeFilter); err != nil || found {
		t.Fatalf("expected active element to be hidden, found=%v err=%v", found, err)
	}

	deletedFilter := types.FilterExpression{}
	storage.AddIDFilter(&deletedFilter, element.ID)
	storage.AddDeletedFilter(&deletedFilter, true)
	deleted, found, err := storage.GetElement(&deletedFilter)
	if err != nil || !found || deleted.DeletedAt == nil {
		t.Fatalf("expected deleted element, found=%v err=%v element=%#v", found, err, deleted)
	}

	if notFound, err := storage.Restore(deleted); err != nil || notFound {
		t.Fatalf("Restore returned notFound=%v err=%v", notFound, err)
	}
	if deleted.DeletedAt != nil {
		t.Fatalf("expected restored deletion timestamp to be nil, got %v", deleted.DeletedAt)
	}

	if notFound, err := storage.Delete(deleted); err != nil || notFound {
		t.Fatalf("second Delete returned notFound=%v err=%v", notFound, err)
	}
	if notFound, err := storage.Prune(deleted); err != nil || notFound {
		t.Fatalf("Prune returned notFound=%v err=%v", notFound, err)
	}
	if _, found, err := storage.GetElement(&deletedFilter); err != nil || found {
		t.Fatalf("expected pruned element to be gone, found=%v err=%v", found, err)
	}
}

func TestStorageCustomIDGenerator(t *testing.T) {
	t.Parallel()

	next := 100
	storage := NewStorageWithIDGenerator[string, *memoryStringIDResource](func() string {
		next++
		return fmt.Sprintf("item-%d", next)
	})
	element := &memoryStringIDResource{Name: "custom"}

	if notFound, err := storage.Save(element); err != nil || notFound {
		t.Fatalf("Save returned notFound=%v err=%v", notFound, err)
	}
	if element.ID == "" {
		t.Fatal("expected custom generated string ID")
	}
}

func TestStorageValidationRejectsUnknownFields(t *testing.T) {
	t.Parallel()

	storage := NewStorage[int, *memoryTestResource]()
	filter := &types.FilterExpression{Operator: types.FilterEQ, Field: "missing", Value: 1}
	if err := storage.ValidateFilter(filter); err == nil {
		t.Fatal("expected invalid filter error")
	}

	sortExpression := &types.SortExpression{Sort: []types.Sort{{Field: "missing"}}}
	if err := storage.ValidateSort(sortExpression); err == nil {
		t.Fatal("expected invalid sort error")
	}
}
