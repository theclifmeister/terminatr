package modannot_test

import (
	"slices"
	"testing"

	"github.com/theclifmeister/terminatr/internal/modannot"
	"github.com/theclifmeister/terminatr/internal/modapi"
)

func TestRolesMatchAPI(t *testing.T) {
	if !slices.Equal(modannot.Roles, modapi.Roles) {
		t.Fatalf("modannot.Roles %v != modapi.Roles %v", modannot.Roles, modapi.Roles)
	}
}
