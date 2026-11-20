package e2e

import (
	"os"
	"testing"
)

func TestStub(t *testing.T) {
	t.Logf("GATEWAY_URL=%q CONFIG=%q", os.Getenv("GATEWAY_URL"), os.Getenv("CONFIG"))
}
