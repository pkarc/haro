package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/pkarc/haro/core"
)

// Asistente con IA. La construcción del contexto y la extracción de la
// propuesta están en core (core/assist.go); aquí vive solo la llamada a la API
// de Anthropic, que es lo único que sale de la máquina.
//
// Envía la configuración (con sus contraseñas y direcciones) a la API de
// Anthropic. Por eso es opt-in: `haro serve --assistant` o el comando `haro
// assist`. La clave (ANTHROPIC_API_KEY o un perfil de `ant auth login`) la lee
// el SDK en el servidor local; nunca llega al navegador.

// defaultAssistantModel es el modelo del asistente salvo que se indique otro.
const defaultAssistantModel = "claude-opus-5-5"

// assistResult resume cómo terminó una respuesta.
type assistResult struct {
	StopReason      string
	RefusalCategory string
	InputTokens     int64
	OutputTokens    int64
}

// streamer envía una conversación al modelo y entrega la respuesta por trozos.
// La implementación real usa el SDK de Anthropic; los tests, una falsa.
type streamer interface {
	Stream(ctx context.Context, system string, msgs []core.AssistMessage, onText func(string) error) (assistResult, error)
}

type anthropicStreamer struct {
	client anthropic.Client
	model  string
}

func newAnthropicStreamer(model string, opts ...option.RequestOption) *anthropicStreamer {
	if model == "" {
		model = defaultAssistantModel
	}
	return &anthropicStreamer{client: anthropic.NewClient(opts...), model: model}
}

func (a *anthropicStreamer) Stream(ctx context.Context, system string, msgs []core.AssistMessage, onText func(string) error) (assistResult, error) {
	params := make([]anthropic.MessageParam, len(msgs))
	for i, m := range msgs {
		block := anthropic.NewTextBlock(m.Content)
		if m.Role == "assistant" {
			params[i] = anthropic.NewAssistantMessage(block)
		} else {
			params[i] = anthropic.NewUserMessage(block)
		}
	}
	// Pensamiento adaptativo; el prompt de sistema es estable y se cachea.
	adaptive := anthropic.ThinkingConfigAdaptiveParam{}
	stream := a.client.Messages.NewStreaming(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(a.model),
		MaxTokens: 32000,
		System:    []anthropic.TextBlockParam{{Text: system, CacheControl: anthropic.NewCacheControlEphemeralParam()}},
		Thinking:  anthropic.ThinkingConfigParamUnion{OfAdaptive: &adaptive},
		Messages:  params,
	})
	defer stream.Close()
	var res assistResult
	for stream.Next() {
		ev := stream.Current()
		switch ev.Type {
		case "message_start":
			res.InputTokens = ev.Message.Usage.InputTokens
		case "content_block_delta":
			if ev.Delta.Type == "text_delta" && ev.Delta.Text != "" {
				if err := onText(ev.Delta.Text); err != nil {
					return res, err
				}
			}
		case "message_delta":
			if ev.Delta.StopReason != "" {
				res.StopReason = string(ev.Delta.StopReason)
				res.RefusalCategory = string(ev.Delta.StopDetails.Category)
			}
			res.OutputTokens = ev.Usage.OutputTokens
		}
	}
	if err := stream.Err(); err != nil {
		return res, assistError(err)
	}
	return res, nil
}

// assistError traduce los errores del SDK a mensajes útiles para el usuario.
func assistError(err error) error {
	var apierr *anthropic.Error
	if errors.As(err, &apierr) {
		switch apierr.StatusCode {
		case 401:
			return errors.New("la API de Anthropic rechazó las credenciales: define ANTHROPIC_API_KEY o ejecuta `ant auth login`")
		case 403:
			return errors.New("la API de Anthropic denegó el acceso (permisos o modelo no disponible para tu cuenta)")
		case 404:
			return errors.New("modelo no encontrado en la API de Anthropic: revisa --assistant-model")
		case 429:
			return errors.New("límite de uso de la API de Anthropic alcanzado: espera un momento y reintenta")
		}
		return fmt.Errorf("error de la API de Anthropic (%d): %s", apierr.StatusCode, apierr.Error())
	}
	if strings.Contains(err.Error(), "could not resolve authentication") || strings.Contains(err.Error(), "apiKey") {
		return errors.New("faltan credenciales de Anthropic: define ANTHROPIC_API_KEY o ejecuta `ant auth login`")
	}
	return err
}

// Límites de la conversación: protegen el coste y el tamaño de la petición.
const (
	maxAssistMessages = 40
	maxAssistHistory  = 200 << 10
)

// validateConversation comprueba que la conversación es válida (alterna roles y
// termina con un turno del usuario) y no es desmesurada.
func validateConversation(msgs []core.AssistMessage) error {
	if len(msgs) == 0 {
		return errors.New("falta la petición del usuario")
	}
	if len(msgs) > maxAssistMessages {
		return fmt.Errorf("la conversación tiene más de %d mensajes: empieza una nueva", maxAssistMessages)
	}
	total := 0
	for i, m := range msgs {
		want := "user"
		if i%2 == 1 {
			want = "assistant"
		}
		if m.Role != want {
			return fmt.Errorf("el mensaje %d debe ser del rol %q", i+1, want)
		}
		if strings.TrimSpace(m.Content) == "" {
			return fmt.Errorf("el mensaje %d está vacío", i+1)
		}
		total += len(m.Content)
	}
	if msgs[len(msgs)-1].Role != "user" {
		return errors.New("la conversación debe terminar con un mensaje del usuario")
	}
	if total > maxAssistHistory {
		return fmt.Errorf("la conversación ocupa más de %d KB: empieza una nueva", maxAssistHistory>>10)
	}
	return nil
}

// assistOutcome es el resultado completo de un turno del asistente.
type assistOutcome struct {
	Text     string
	Proposal core.Proposal
	// IssuesAfter son los problemas de la configuración con la propuesta aplicada.
	IssuesAfter []core.Issue
	Result      assistResult
}

// runAssist ejecuta un turno: construye el contexto con la configuración
// actual, envía la conversación, va entregando el texto y al final extrae la
// propuesta y analiza cómo quedaría la configuración.
func runAssist(ctx context.Context, st streamer, srcs []core.Source, msgs []core.AssistMessage, lang core.Lang, onText func(string) error) (assistOutcome, error) {
	var out assistOutcome
	if err := validateConversation(msgs); err != nil {
		return out, err
	}
	a := core.AnalyzeConfigLang(srcs, lang)
	cx, err := core.AssistContext(srcs, a, lang)
	if err != nil {
		return out, err
	}
	send := append([]core.AssistMessage{}, msgs...)
	last := &send[len(send)-1]
	last.Content = cx + "\n\n" + last.Content

	var text strings.Builder
	res, err := st.Stream(ctx, core.AssistSystemPrompt(lang), send, func(t string) error {
		text.WriteString(t)
		return onText(t)
	})
	out.Text, out.Result = text.String(), res
	if err != nil {
		return out, err
	}
	out.Proposal = core.ExtractProposal(out.Text, srcs)
	if len(out.Proposal.Changes) > 0 {
		after := append([]core.Source{}, srcs...)
		for _, ch := range out.Proposal.Changes {
			after[ch.File].Text = ch.Text
		}
		out.IssuesAfter = core.LintLang(core.NewConfig(after).Merge(), lang)
	}
	return out, nil
}

// ---------- haro assist ----------

func runAssistCmd(args []string) error {
	fs := flag.NewFlagSet("assist", flag.ExitOnError)
	langFlag := fs.String("lang", "", "idioma: es o en (por defecto, el del entorno)")
	model := fs.String("model", envOr("HARO_ASSISTANT_MODEL", defaultAssistantModel), "modelo de Anthropic")
	write := fs.Bool("write", false, "escribe la propuesta (valida la configuración completa y deja copia .bak)")
	yes := fs.Bool("yes", false, "con --write, no pide confirmación")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Uso: haro assist [opciones] \"petición\" [archivo|directorio]...")
		fmt.Fprintln(fs.Output(), "Pregunta o pide cambios al asistente. ENVÍA LA CONFIGURACIÓN a la API de Anthropic.")
		fmt.Fprintln(fs.Output(), "Necesita ANTHROPIC_API_KEY (o `ant auth login`). Sin --write solo muestra el diff propuesto.")
		fs.PrintDefaults()
	}
	pos, err := parseArgs(fs, args)
	if errors.Is(err, errHelpShown) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(pos) < 1 {
		fs.Usage()
		return exitError(2)
	}
	request, paths := pos[0], pos[1:]
	lang, err := resolveLang(*langFlag)
	if err != nil {
		return err
	}
	for _, p := range paths {
		if p == "-" {
			return errors.New("haro assist no lee de stdin; indica archivos o directorios")
		}
	}
	srcs, err := readSources(paths)
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "haro: la configuración se envía a la API de Anthropic (modelo "+*model+")")
	st := newAnthropicStreamer(*model)
	out, err := runAssist(context.Background(), st, srcs, []core.AssistMessage{{Role: "user", Content: request}}, lang, func(t string) error {
		_, err := io.WriteString(os.Stdout, t)
		return err
	})
	fmt.Println()
	if err != nil {
		return err
	}
	return finishAssistCLI(out, srcs, paths, *write, *yes)
}

// finishAssistCLI muestra el diff de la propuesta y, con --write, la guarda.
func finishAssistCLI(out assistOutcome, srcs []core.Source, paths []string, write, yes bool) error {
	switch out.Result.StopReason {
	case "refusal":
		return fmt.Errorf("el modelo declinó la petición (%s)", out.Result.RefusalCategory)
	case "max_tokens":
		fmt.Fprintln(os.Stderr, "haro: la respuesta se cortó por el límite de tokens")
	}
	p := out.Proposal
	if p.Truncated {
		fmt.Fprintln(os.Stderr, "haro: un bloque de configuración quedó cortado y se descartó")
	}
	if p.Unmatched > 0 {
		fmt.Fprintf(os.Stderr, "haro: %d bloque(s) de configuración no se pudieron asociar a un archivo y se descartaron\n", p.Unmatched)
	}
	if len(p.Changes) == 0 {
		return nil
	}
	fmt.Println("\n── propuesta ──")
	for _, ch := range p.Changes {
		fmt.Print(core.UnifiedDiff(ch.Path, srcs[ch.File].Text, ch.Text, 3))
	}
	errs := 0
	for _, i := range out.IssuesAfter {
		if i.Severity == core.SevError {
			errs++
		}
	}
	fmt.Printf("\nTras aplicarla: %s.\n", plural(errs, "error", "errores"))
	if !write {
		fmt.Println("Para escribirla: repite con --write (o aplícala desde la interfaz).")
		return nil
	}
	if !yes {
		fmt.Print("¿Escribir estos cambios? [s/N] ")
		var ans string
		fmt.Scanln(&ans)
		if a := strings.ToLower(strings.TrimSpace(ans)); a != "s" && a != "y" && a != "si" && a != "sí" && a != "yes" {
			fmt.Println("No se escribió nada.")
			return nil
		}
	}
	files := make([]core.Source, len(p.Changes))
	for i, ch := range p.Changes {
		files[i] = core.Source{Path: ch.Path, Text: ch.Text}
	}
	s := &server{paths: paths, allowWrite: true}
	if bin, err := exec.LookPath("haproxy"); err == nil {
		s.haproxyBin = bin
	}
	res, err := s.put(context.Background(), files)
	if err != nil {
		return err
	}
	if res.Err != "" {
		msg := res.Err
		if res.Check != nil && res.Check.Output != "" {
			msg += "\n" + res.Check.Output
		}
		if res.Issue != nil {
			msg += fmt.Sprintf("\n%s:%d: %s", res.Path, res.Issue.Line, res.Issue.Message)
		}
		return errors.New("no se guardó: " + msg)
	}
	for _, sv := range res.Saved {
		fmt.Printf("Guardado %s (copia en %s)\n", sv.Path, sv.Backup)
	}
	return nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
