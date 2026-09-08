package render

import (
	"testing"

	"github.com/zc310/ofd/internal/models"
)

func TestRenderBudgetLimitsCompositeExpansion(t *testing.T) {
	var budget renderBudget
	budget.reset()

	for i := 0; i < maxCompositeExpansions; i++ {
		if !budget.allowComposite(models.StID(i + 1)) {
			t.Fatalf("composite expansion %d was rejected", i)
		}
	}
	if budget.allowComposite(models.StID(maxCompositeExpansions + 1)) {
		t.Fatal("composite expansion beyond the global limit was accepted")
	}
}

func TestRenderBudgetLimitsRepeatedCompositeResource(t *testing.T) {
	var budget renderBudget
	budget.reset()
	resourceID := models.StID(1)

	for i := 0; i < maxCompositeResourceExpansions; i++ {
		if !budget.allowComposite(resourceID) {
			t.Fatalf("composite resource expansion %d was rejected", i)
		}
	}
	if budget.allowComposite(resourceID) {
		t.Fatal("repeated composite resource expansion beyond the limit was accepted")
	}
}

func TestRenderBudgetLimitsPatternTilesAndOffscreenPixels(t *testing.T) {
	var budget renderBudget
	budget.reset()

	if !budget.allowPatternTiles(maxPatternTilesPerRender - 1) {
		t.Fatal("pattern tiles within the budget were rejected")
	}
	if budget.allowPatternTiles(2) {
		t.Fatal("pattern tiles beyond the budget were accepted")
	}

	budget.reset()
	if !budget.allowOffscreenPixels(10, 10, 10) {
		t.Fatal("offscreen allocation within the budget was rejected")
	}
	if budget.allowOffscreenPixels(1e9, 1e9, 1200) {
		t.Fatal("offscreen allocation beyond the budget was accepted")
	}
}

func TestRenderBudgetResetClearsUsage(t *testing.T) {
	var budget renderBudget
	budget.reset()
	if !budget.allowComposite(models.StID(1)) || !budget.allowPatternTiles(1) || !budget.allowOffscreenPixels(10, 10, 10) {
		t.Fatal("failed to consume render budget")
	}

	budget.reset()
	if !budget.allowComposite(models.StID(1)) || !budget.allowPatternTiles(1) || !budget.allowOffscreenPixels(10, 10, 10) {
		t.Fatal("render budget was not reset")
	}
}
