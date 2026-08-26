package api

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/dougkusanagi/dev-lan/internal/metrics"
)

type contractManifestSpec struct {
	ProtocolVersion int `json:"protocolVersion"`
	Types           map[string]struct {
		GoType string `json:"goType"`
		Fields map[string]struct {
			Type     string `json:"type"`
			Required bool   `json:"required"`
		} `json:"fields"`
	} `json:"types"`
}

func TestContractManifestMatchesGoJSONViews(t *testing.T) {
	var manifest contractManifestSpec
	if err := json.Unmarshal(ContractManifest(), &manifest); err != nil {
		t.Fatalf("contrato inválido: %v", err)
	}
	if manifest.ProtocolVersion != ProtocolVersion {
		t.Fatalf("versão do contrato %d diverge da API %d", manifest.ProtocolVersion, ProtocolVersion)
	}

	types := map[string]reflect.Type{
		"ProjectView":           reflect.TypeOf(ProjectView{}),
		"SystemStatusView":      reflect.TypeOf(SystemStatusView{}),
		"PHPVersionView":        reflect.TypeOf(PHPVersionView{}),
		"DoctorCheckView":       reflect.TypeOf(DoctorCheckView{}),
		"GlobalConfigView":      reflect.TypeOf(GlobalConfigView{}),
		"ProjectConfigUpdate":   reflect.TypeOf(ProjectConfigUpdate{}),
		"metrics.Snapshot":      reflect.TypeOf(metrics.Snapshot{}),
		"metrics.Bucket":        reflect.TypeOf(metrics.Bucket{}),
		"metrics.TrafficPoint":  reflect.TypeOf(metrics.TrafficPoint{}),
		"metrics.RouteSnapshot": reflect.TypeOf(metrics.RouteSnapshot{}),
	}

	for name, spec := range manifest.Types {
		goType, ok := types[spec.GoType]
		if !ok {
			t.Fatalf("tipo Go %q do recurso %q não foi registrado no teste", spec.GoType, name)
		}
		goFields := jsonFields(goType)
		if len(goFields) != len(spec.Fields) {
			t.Fatalf("%s: contrato tem %d campos, Go tem %d (%v vs %v)", name, len(spec.Fields), len(goFields), sortedKeys(spec.Fields), sortedKeys(goFields))
		}
		for field, contractField := range spec.Fields {
			goField, ok := goFields[field]
			if !ok {
				t.Errorf("%s: campo %q existe no contrato, mas não no tipo Go", name, field)
				continue
			}
			if contractField.Required != goField.required {
				t.Errorf("%s.%s: required=%t no contrato, mas required=%t no Go", name, field, contractField.Required, goField.required)
			}
			if !compatibleJSONType(goField.typ, contractField.Type) {
				t.Errorf("%s.%s: type=%q no contrato, mas Go expõe %s", name, field, contractField.Type, reflectJSONKind(goField.typ))
			}
		}
	}
}

type jsonField struct {
	required bool
	typ      reflect.Type
}

func jsonFields(value reflect.Type) map[string]jsonField {
	result := make(map[string]jsonField)
	for i := 0; i < value.NumField(); i++ {
		field := value.Field(i)
		tag := field.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name := strings.Split(tag, ",")[0]
		if name == "" {
			continue
		}
		result[name] = jsonField{required: !strings.Contains(tag, "omitempty"), typ: field.Type}
	}
	return result
}

func compatibleJSONType(goType reflect.Type, contractType string) bool {
	return reflectJSONKind(goType) == contractJSONKind(contractType)
}

func reflectJSONKind(goType reflect.Type) string {
	if goType.Kind() == reflect.Pointer {
		goType = goType.Elem()
	}
	switch goType.Kind() {
	case reflect.Bool:
		return "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return "number"
	case reflect.String:
		return "string"
	case reflect.Array, reflect.Slice:
		return "array"
	case reflect.Struct, reflect.Map:
		return "object"
	default:
		return goType.Kind().String()
	}
}

func contractJSONKind(contractType string) string {
	if strings.Contains(contractType, "[]") {
		return "array"
	}
	if strings.Contains(contractType, "boolean") {
		return "boolean"
	}
	if strings.Contains(contractType, "number") {
		return "number"
	}
	if strings.Contains(contractType, "string") || strings.Contains(contractType, "'") {
		return "string"
	}
	if strings.Contains(contractType, "Project") || strings.Contains(contractType, "Mode") || strings.Contains(contractType, "Range") || strings.Contains(contractType, "State") {
		return "string"
	}
	return "string"
}

func sortedKeys[T any](values map[string]T) []string {
	result := make([]string, 0, len(values))
	for key := range values {
		result = append(result, key)
	}
	return result
}
