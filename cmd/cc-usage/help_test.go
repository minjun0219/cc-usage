package main

import (
	"reflect"
	"strings"
	"testing"

	"cc-usage/internal/config"
)

func TestConfigFieldsListsEveryField(t *testing.T) {
	// `cc-usage config` 는 README 없이 바이너리만 가진 쪽이 보는 필드 목록이다.
	// Config 에 필드를 더하고 여기를 잊으면 그 필드는 바이너리 표면에서 사라진다.
	typ := reflect.TypeOf(config.Config{})
	for i := 0; i < typ.NumField(); i++ {
		tag := strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]
		if tag == "" || tag == "-" {
			continue
		}
		if !strings.Contains(configFields, "\n  "+tag+" ") {
			t.Errorf("configFields 에 %q 가 없다", tag)
		}
	}
}

func TestHelpNamesConfigAndExtraCommands(t *testing.T) {
	t.Setenv("CC_USAGE_CONFIG", "/nowhere/cc-usage.json")
	for name, h := range map[string]string{"--help": helpText(), "statusline --help": statuslineHelpText()} {
		for _, want := range []string{"/nowhere/cc-usage.json", "CC_USAGE_CONFIG", "extra_commands", "{{session_id}}", "{{cwd}}", "timeout_ms", "cc-usage doctor"} {
			if !strings.Contains(h, want) {
				t.Errorf("%s 에 %q 가 없다", name, want)
			}
		}
	}
}
