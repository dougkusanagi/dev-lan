package config

import (
	"testing"

	"github.com/dougkusanagi/dev-lan/internal/application/ports"
	"github.com/dougkusanagi/dev-lan/internal/portcontract"
)

func TestFileStoreSatisfiesApplicationPortContract(t *testing.T) {
	portcontract.RunStoreContract(t, func(t *testing.T) ports.Store {
		return NewStore(t.TempDir())
	})
}
