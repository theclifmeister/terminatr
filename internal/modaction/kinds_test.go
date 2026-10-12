package modaction_test

import (
	"testing"

	"github.com/theclifmeister/terminatr/internal/modaction"
	"github.com/theclifmeister/terminatr/internal/project"
)

// TestKinds: the ask kinds are the inbox's. (An external test: project
// imports mod, which imports modaction.)
func TestKinds(t *testing.T) {
	if modaction.KindDelegate != project.KindDelegate || modaction.KindAccept != project.KindAccept || modaction.KindSendBack != project.KindSendBack {
		t.Error("the ask kinds differ from the inbox's")
	}
}
