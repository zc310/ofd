package render

import "testing"

func TestLRUCacheEvictsLeastRecentlyUsed(t *testing.T) {
	cache := newLRU[string, int](2)
	cache.add("a", 1)
	cache.add("b", 2)
	if _, ok := cache.get("a"); !ok {
		t.Fatal("expected a to be cached")
	}
	cache.add("c", 3)
	if _, ok := cache.get("b"); ok {
		t.Fatal("expected b to be evicted")
	}
	if value, ok := cache.get("a"); !ok || value != 1 {
		t.Fatalf("expected a to remain cached, got %d, %v", value, ok)
	}
	if value, ok := cache.get("c"); !ok || value != 3 {
		t.Fatalf("expected c to be cached, got %d, %v", value, ok)
	}
}

func TestLRUCacheUpdatesExistingValue(t *testing.T) {
	cache := newLRU[string, int](1)
	cache.add("a", 1)
	cache.add("a", 2)
	if cache.len() != 1 {
		t.Fatalf("cache length = %d, want 1", cache.len())
	}
	if value, ok := cache.get("a"); !ok || value != 2 {
		t.Fatalf("expected updated value, got %d, %v", value, ok)
	}
}
