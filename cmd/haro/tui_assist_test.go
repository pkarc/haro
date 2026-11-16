package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pkarc/haro/core"
)

func TestTUISaveValidatesAndKeepsBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "haproxy.cfg")
	if err := os.WriteFile(path, []byte(strings.Replace(assistCfgText, "default_backend nope", "default_backend app", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	save := tuiSave([]string{path})

	// Una propuesta con un error de referencia se rechaza y no toca el archivo.
	before, _ := os.ReadFile(path)
	err := save([]core.FileChange{{Path: path, Text: assistCfgText}})
	if err == nil || !strings.Contains(err.Error(), "not saved") {
		t.Fatalf("debía rechazarse: %v", err)
	}
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Error("una propuesta rechazada no debe escribir nada")
	}

	// Una válida se guarda y deja .bak con el contenido anterior.
	newText := strings.Replace(string(before), "timeout client 30s", "timeout client 60s", 1)
	if err := save([]core.FileChange{{Path: path, Text: newText}}); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != newText {
		t.Error("no se escribió la propuesta")
	}
	if bak, _ := os.ReadFile(path + ".bak"); string(bak) != string(before) {
		t.Error("falta la copia .bak con el contenido anterior")
	}
}

func TestTUIAssistConnectsRunAssist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "haproxy.cfg")
	os.WriteFile(path, []byte(assistCfgText), 0o644)
	f := &fakeStreamer{reply: fixReply, result: assistResult{StopReason: "end_turn"}}
	load := func() ([]core.Source, error) { return readSources([]string{path}) }
	var streamed strings.Builder
	out, err := tuiAssist(f, []string{path}, load)(context.Background(),
		[]core.AssistMessage{{Role: "user", Content: "arregla"}}, core.LangES, func(s string) { streamed.WriteString(s) })
	if err != nil {
		t.Fatal(err)
	}
	if streamed.String() != fixReply || out.Text != fixReply || out.StopReason != "end_turn" {
		t.Errorf("salida: %+v", out)
	}
	if len(out.Proposal.Changes) != 1 || out.Proposal.Changes[0].Path != path {
		t.Errorf("propuesta: %+v", out.Proposal)
	}
	if !strings.Contains(f.msgs[len(f.msgs)-1].Content, "default_backend nope") {
		t.Error("el modelo debe ver la configuración vigente")
	}
}
