package architecture

import (
	"reflect"
	"testing"

	"github.com/dougkusanagi/dev-lan/internal/app"
)

func TestAppDoesNotExposeLegacyCaddyFields(t *testing.T) {
	typeOfApp := reflect.TypeOf(app.App{})
	for _, name := range []string{"WindowsCaddy", "WSLCaddy"} {
		field, found := typeOfApp.FieldByName(name)
		if found && field.PkgPath == "" {
			t.Errorf("app.App ainda expõe o campo legado %s", name)
		}
	}
	if field, found := typeOfApp.FieldByName("Caddy"); !found || field.PkgPath != "" {
		t.Fatal("app.App perdeu a dependência pública do único Caddy operacional")
	}
}
