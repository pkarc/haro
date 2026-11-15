package core

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
)

// Operaciones de edición. Todas las interfaces (modo visual de la web, TUI,
// MCP, asistente) editan a través de Apply para que el cambio en el texto sea
// siempre mínimo: solo se toca lo que la operación necesita, y los archivos que
// la operación no menciona no se tocan en absoluto.
//
// Los índices de sección son los de la vista unificada (Config.Merge), los
// mismos que usan Issue.Section, LineExplanation.Section y la topología. Los
// índices de línea (LineIndex) son posiciones dentro de Section.Lines, que
// incluye comentarios y líneas en blanco. Las operaciones se aplican en orden,
// y cada una ve el resultado de la anterior.

//go:embed knowledge/templates.json
var templatesJSON []byte

// SectionTemplate es la plantilla de una sección nueva. Las líneas que
// contienen {{backend}} se omiten si la configuración no tiene ningún backend.
type SectionTemplate struct {
	Name  string   `json:"name"`
	Lines []string `json:"lines"`
}

// Templates son las plantillas de sección por tipo, cargadas al iniciar.
var Templates = mustLoadTemplates()

func mustLoadTemplates() map[string]SectionTemplate {
	var t map[string]SectionTemplate
	if err := json.Unmarshal(templatesJSON, &t); err != nil {
		panic(fmt.Sprintf("haro: plantillas inválidas: %v", err))
	}
	return t
}

// Nombres de operación.
const (
	OpAddSection      = "add_section"
	OpRemoveSection   = "remove_section"
	OpMoveSection     = "move_section"
	OpRenameSection   = "rename_section"
	OpAddDirective    = "add_directive"
	OpUpdateDirective = "update_directive"
	OpRemoveDirective = "remove_directive"
	OpMoveDirective   = "move_directive"
)

// Op es una operación de edición serializable a JSON. Según Op se usan unos
// campos u otros:
//
//	add_section      Type, Name?, Template?, File?, At?
//	remove_section   Section
//	move_section     Section, To, File?
//	rename_section   Section, Name, UpdateRefs?
//	add_directive    Section, Text, At?
//	update_directive Section, LineIndex, Text
//	remove_directive Section, LineIndex
//	move_directive   Section, LineIndex, Delta
//
// Al crear o mover una sección, At y To son posiciones en la vista unificada.
// File elige el archivo de destino; sin File, add_section deduce el archivo de
// la sección que ocupa esa posición (o usa el último archivo, y el primero para
// global) y move_section deja la sección en su archivo.
type Op struct {
	Op         string `json:"op"`
	Section    int    `json:"section,omitempty"`
	Type       string `json:"type,omitempty"`
	Name       string `json:"name,omitempty"`
	Template   bool   `json:"template,omitempty"`
	File       *int   `json:"file,omitempty"`
	At         *int   `json:"at,omitempty"`
	To         *int   `json:"to,omitempty"`
	Text       string `json:"text,omitempty"`
	LineIndex  int    `json:"lineIndex,omitempty"`
	Delta      int    `json:"delta,omitempty"`
	UpdateRefs bool   `json:"updateRefs,omitempty"`
}

// FileChange es el nuevo texto de un archivo modificado por Apply.
type FileChange struct {
	File int    `json:"file"`
	Path string `json:"path"`
	Text string `json:"text"`
}

// ApplyError indica qué operación falló. Si una falla, no se aplica ninguna.
type ApplyError struct {
	Index int    `json:"index"`
	Op    string `json:"op"`
	Msg   string `json:"message"`
}

func (e *ApplyError) Error() string {
	return fmt.Sprintf("operación %d (%s): %s", e.Index+1, e.Op, e.Msg)
}

// Apply aplica las operaciones sobre una copia de cfg y devuelve la nueva
// configuración (reanalizada, con los números de línea al día) y los archivos
// cuyo texto cambió. cfg no se modifica. Si una operación falla no se aplica
// ninguna y el error es un *ApplyError.
func Apply(cfg *Config, ops ...Op) (*Config, []FileChange, error) {
	before := make([]Source, len(cfg.Files))
	for i, f := range cfg.Files {
		before[i] = Source{Path: f.Path, Text: Serialize(f.Doc)}
	}
	e := &editor{cfg: NewConfig(before)}
	for i, op := range ops {
		if err := e.apply(op); err != nil {
			return nil, nil, &ApplyError{Index: i, Op: op.Op, Msg: err.Error()}
		}
	}
	after := make([]Source, len(e.cfg.Files))
	var changes []FileChange
	for i, f := range e.cfg.Files {
		after[i] = Source{Path: f.Path, Text: Serialize(f.Doc)}
		if after[i].Text != before[i].Text {
			changes = append(changes, FileChange{File: i, Path: f.Path, Text: after[i].Text})
		}
	}
	return NewConfig(after), changes, nil
}

type editor struct {
	cfg *Config
}

func (e *editor) apply(op Op) error {
	switch op.Op {
	case OpAddSection:
		return e.addSection(op)
	case OpRemoveSection:
		return e.removeSection(op)
	case OpMoveSection:
		return e.moveSection(op)
	case OpRenameSection:
		return e.renameSection(op)
	case OpAddDirective:
		return e.addDirective(op)
	case OpUpdateDirective:
		return e.updateDirective(op)
	case OpRemoveDirective:
		return e.removeDirective(op)
	case OpMoveDirective:
		return e.moveDirective(op)
	}
	return fmt.Errorf("operación desconocida")
}

// --- Bloques --------------------------------------------------------------

// El parser deja los comentarios que van justo encima de una cabecera al final
// de la sección anterior. Para mover o borrar secciones hay que llevárselos
// con ella, así que cada archivo se descompone temporalmente en bloques.

type block struct {
	lead  []*Line // comentarios sin indentar pegados a la cabecera
	sec   *Section
	body  []*Line // líneas de la sección, sin los comentarios del bloque siguiente
	dirty bool    // sección nueva o movida: hay que asegurar su separación
}

type fileBlocks struct {
	pre    []*Line
	blocks []*block
	endsNL bool // el archivo original terminaba con salto de línea
}

// splitLead separa el final de lines en (resto, comentarios pegados a la
// siguiente cabecera).
func splitLead(lines []*Line) (rest, lead []*Line) {
	k := len(lines)
	for k > 0 && lines[k-1].Kind == KindComment && lines[k-1].Indent == "" {
		k--
	}
	// Expresión completa de corte: un append posterior no debe pisar los comentarios.
	return lines[:k:k], lines[k:]
}

func endsBlank(lines []*Line) bool {
	return len(lines) > 0 && lines[len(lines)-1].Kind == KindBlank
}

func explode(doc *Doc) *fileBlocks {
	fb := &fileBlocks{}
	rest, lead := doc.Pre, []*Line(nil)
	if len(doc.Sections) > 0 {
		rest, lead = splitLead(doc.Pre)
	}
	fb.pre = rest
	for i, s := range doc.Sections {
		b := &block{lead: lead, sec: s, body: s.Lines}
		if i+1 < len(doc.Sections) {
			b.body, lead = splitLead(s.Lines)
		}
		fb.blocks = append(fb.blocks, b)
	}
	if n := len(fb.blocks); n > 0 {
		fb.endsNL = endsBlank(fb.blocks[n-1].body)
	} else {
		fb.endsNL = endsBlank(fb.pre)
	}
	return fb
}

func blank() *Line { return &Line{Kind: KindBlank} }

// implode vuelve a escribir los bloques en el documento. Solo añade líneas en
// blanco alrededor de lo nuevo o movido y conserva el salto de línea final.
func (fb *fileBlocks) implode(doc *Doc) {
	for i, b := range fb.blocks {
		if !b.dirty {
			continue
		}
		if i == 0 {
			if len(fb.pre) > 0 && !endsBlank(fb.pre) {
				fb.pre = append(fb.pre, blank())
			}
		} else if prev := fb.blocks[i-1]; !endsBlank(prev.body) {
			prev.body = append(prev.body, blank())
		}
		if i < len(fb.blocks)-1 && !endsBlank(b.body) {
			b.body = append(b.body, blank())
		}
	}
	if n := len(fb.blocks); n > 0 {
		if last := fb.blocks[n-1]; fb.endsNL && !endsBlank(last.body) {
			last.body = append(last.body, blank())
		}
	} else if fb.endsNL && !endsBlank(fb.pre) {
		fb.pre = append(fb.pre, blank())
	}

	doc.Pre = fb.pre
	if len(fb.blocks) > 0 {
		doc.Pre = append(append([]*Line{}, fb.pre...), fb.blocks[0].lead...)
	}
	doc.Sections = make([]*Section, len(fb.blocks))
	for i, b := range fb.blocks {
		lines := append([]*Line{}, b.body...)
		if i+1 < len(fb.blocks) {
			lines = append(lines, fb.blocks[i+1].lead...)
		}
		b.sec.Lines = lines
		doc.Sections[i] = b.sec
	}
}

// --- Localización ---------------------------------------------------------

func (e *editor) total() int {
	n := 0
	for _, f := range e.cfg.Files {
		n += len(f.Doc.Sections)
	}
	return n
}

// locate traduce un índice de la vista unificada a (archivo, posición en el archivo).
func (e *editor) locate(i int) (file, local int, err error) {
	if i < 0 {
		return 0, 0, fmt.Errorf("la sección %d no existe", i)
	}
	for fi, f := range e.cfg.Files {
		if i < len(f.Doc.Sections) {
			return fi, i, nil
		}
		i -= len(f.Doc.Sections)
	}
	return 0, 0, fmt.Errorf("la sección no existe (hay %d)", e.total())
}

func (e *editor) section(i int) (*Section, error) {
	fi, local, err := e.locate(i)
	if err != nil {
		return nil, err
	}
	return e.cfg.Files[fi].Doc.Sections[local], nil
}

// fileStart es el índice unificado de la primera sección del archivo fi.
func (e *editor) fileStart(fi int) int {
	n := 0
	for _, f := range e.cfg.Files[:fi] {
		n += len(f.Doc.Sections)
	}
	return n
}

func (e *editor) checkFile(fi int) error {
	if fi < 0 || fi >= len(e.cfg.Files) {
		return fmt.Errorf("el archivo %d no existe (hay %d)", fi, len(e.cfg.Files))
	}
	return nil
}

func (e *editor) all() []*Section { return e.cfg.Merge().Sections }

// --- Validación de nombres ------------------------------------------------

func validName(name string) error {
	if strings.ContainsAny(name, " \t\r\n#\"'\\") {
		return fmt.Errorf("el nombre «%s» no puede contener espacios, comillas, # ni \\", name)
	}
	return nil
}

// nameKeys son los espacios de nombres que ocupa una sección: un listen es a
// la vez frontend y backend, igual que en el detector.
func nameKeys(typ string) []string {
	if typ == "listen" {
		return []string{"frontend", "backend"}
	}
	return []string{typ}
}

func (e *editor) nameTaken(typ, name string, skip *Section) bool {
	if name == "" {
		return false
	}
	for _, s := range e.all() {
		if s == skip || s.Name != name {
			continue
		}
		for _, a := range nameKeys(s.Type) {
			for _, b := range nameKeys(typ) {
				if a == b {
					return true
				}
			}
		}
	}
	return false
}

func uniqueName(e *editor, typ, base string) string {
	name := base
	for i := 2; e.nameTaken(typ, name, nil); i++ {
		name = fmt.Sprintf("%s_%d", base, i)
	}
	return name
}

// --- Líneas ---------------------------------------------------------------

// detectIndent devuelve la indentación que usa la sección (o, si no tiene
// directivas, su archivo) para que las líneas nuevas encajen con el estilo.
func (e *editor) detectIndent(s *Section) string {
	find := func(secs []*Section) string {
		for _, sec := range secs {
			for _, l := range sec.Lines {
				if l.Kind == KindDirective && l.IndentSet && l.Indent != "" {
					return l.Indent
				}
			}
		}
		return ""
	}
	if ind := find([]*Section{s}); ind != "" {
		return ind
	}
	if ind := find(e.cfg.Files[s.File].Doc.Sections); ind != "" {
		return ind
	}
	return DefaultIndent
}

// parseOpLine interpreta el texto de una directiva o comentario con la
// indentación que tendrá. Rechaza saltos de línea, líneas vacías y cabeceras
// de sección (que cambiarían la estructura del archivo al reescribirlo).
func parseOpLine(text, indent string, file int) (*Line, error) {
	if strings.ContainsAny(text, "\r\n") {
		return nil, fmt.Errorf("el texto debe ser una sola línea")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, fmt.Errorf("el texto está vacío")
	}
	l, sec := ParseLine(indent+text, 0)
	if sec != nil {
		return nil, fmt.Errorf("«%s» es una cabecera de sección, no una directiva; usa add_section", text)
	}
	l.File = file
	l.Indent, l.IndentSet = indent, true
	return l, nil
}

// bodyEnd es la posición donde acaba el contenido propio de la sección: antes
// de las líneas en blanco finales y, si hay otra sección detrás en el archivo,
// de los comentarios que pertenecen a su cabecera.
func (e *editor) bodyEnd(s *Section) int {
	lines := s.Lines
	n := len(lines)
	secs := e.cfg.Files[s.File].Doc.Sections
	if secs[len(secs)-1] != s {
		rest, _ := splitLead(lines)
		n = len(rest)
	}
	for n > 0 && lines[n-1].Kind == KindBlank {
		n--
	}
	return n
}

func (e *editor) lineAt(op Op) (*Section, *Line, error) {
	s, err := e.section(op.Section)
	if err != nil {
		return nil, nil, err
	}
	if op.LineIndex < 0 || op.LineIndex >= len(s.Lines) {
		return nil, nil, fmt.Errorf("la línea %d no existe en la sección (tiene %d)", op.LineIndex, len(s.Lines))
	}
	return s, s.Lines[op.LineIndex], nil
}

func (e *editor) addDirective(op Op) error {
	s, err := e.section(op.Section)
	if err != nil {
		return err
	}
	l, err := parseOpLine(op.Text, e.detectIndent(s), s.File)
	if err != nil {
		return err
	}
	at := e.bodyEnd(s)
	if op.At != nil {
		if *op.At < 0 || *op.At > len(s.Lines) {
			return fmt.Errorf("la posición %d está fuera de la sección (tiene %d líneas)", *op.At, len(s.Lines))
		}
		at = *op.At
	}
	s.Lines = append(s.Lines[:at], append([]*Line{l}, s.Lines[at:]...)...)
	return nil
}

func (e *editor) updateDirective(op Op) error {
	s, old, err := e.lineAt(op)
	if err != nil {
		return err
	}
	if old.Kind == KindBlank {
		return fmt.Errorf("la línea %d está en blanco; usa add_directive", op.LineIndex)
	}
	indent := old.Indent
	if indent == "" {
		indent = e.detectIndent(s)
	}
	l, err := parseOpLine(op.Text, indent, s.File)
	if err != nil {
		return err
	}
	s.Lines[op.LineIndex] = l
	return nil
}

func (e *editor) removeDirective(op Op) error {
	s, _, err := e.lineAt(op)
	if err != nil {
		return err
	}
	s.Lines = append(s.Lines[:op.LineIndex], s.Lines[op.LineIndex+1:]...)
	return nil
}

// moveDirective intercambia la línea con su vecina no vacía |Delta| veces. Así
// las líneas en blanco que agrupan la sección se quedan donde están. Se detiene
// en los límites del contenido de la sección.
func (e *editor) moveDirective(op Op) error {
	s, l, err := e.lineAt(op)
	if err != nil {
		return err
	}
	if l.Kind == KindBlank {
		return fmt.Errorf("no se puede mover una línea en blanco")
	}
	step := 1
	if op.Delta < 0 {
		step = -1
	}
	end := e.bodyEnd(s)
	pos := op.LineIndex
	for n := 0; n < op.Delta*step; n++ {
		j := pos + step
		for j >= 0 && j < end && s.Lines[j].Kind == KindBlank {
			j += step
		}
		if j < 0 || j >= end {
			break
		}
		s.Lines[pos], s.Lines[j] = s.Lines[j], s.Lines[pos]
		pos = j
	}
	return nil
}

// --- Secciones ------------------------------------------------------------

func (e *editor) addSection(op Op) error {
	info, ok := KB.Sections[op.Type]
	if !ok {
		return fmt.Errorf("tipo de sección desconocido: «%s»", op.Type)
	}
	if len(e.cfg.Files) == 0 {
		return fmt.Errorf("no hay archivos donde añadir la sección")
	}
	tpl := Templates[op.Type]
	name := op.Name
	switch info.Named {
	case "none":
		if name != "" {
			return fmt.Errorf("la sección %s no lleva nombre", op.Type)
		}
	case "required":
		if name == "" {
			name = uniqueName(e, op.Type, orDefault(tpl.Name, op.Type))
		}
	}
	if err := validName(name); err != nil {
		return err
	}
	if op.Type == "global" {
		for _, s := range e.all() {
			if s.Type == "global" {
				return fmt.Errorf("ya hay una sección global")
			}
		}
	}
	if e.nameTaken(op.Type, name, nil) {
		return fmt.Errorf("ya existe una sección llamada «%s»", name)
	}

	// Archivo y posición dentro del archivo.
	var fi, local int
	switch {
	case op.File != nil:
		fi = *op.File
		if err := e.checkFile(fi); err != nil {
			return err
		}
		local = e.defaultPosition(fi, op.Type)
		if op.At != nil {
			start := e.fileStart(fi)
			if *op.At < start || *op.At > start+len(e.cfg.Files[fi].Doc.Sections) {
				return fmt.Errorf("la posición %d queda fuera del archivo %d", *op.At, fi)
			}
			local = *op.At - start
		}
	case op.At != nil:
		at := *op.At
		if at < 0 || at > e.total() {
			return fmt.Errorf("la posición %d no es válida (hay %d secciones)", at, e.total())
		}
		if at < e.total() {
			fi, local, _ = e.locate(at)
		} else {
			fi = len(e.cfg.Files) - 1
			local = len(e.cfg.Files[fi].Doc.Sections)
		}
	default:
		fi = len(e.cfg.Files) - 1
		if op.Type == "global" {
			fi = 0
		}
		for gi, f := range e.cfg.Files {
			for _, s := range f.Doc.Sections {
				if op.Type == "defaults" && s.Type == "global" {
					fi = gi
				}
			}
		}
		local = e.defaultPosition(fi, op.Type)
	}

	indent := DefaultIndent
	if ind := e.detectIndent(&Section{File: fi}); ind != "" {
		indent = ind
	}
	header := op.Type
	if name != "" {
		header += " " + QuoteToken(name)
	}
	_, sec := ParseLine(header, 0)
	sec.File = fi
	sec.Lines = []*Line{}
	var body []*Line
	if op.Template {
		backend := ""
		for _, s := range e.all() {
			if s.Type == "backend" && s.Name != "" {
				backend = s.Name
				break
			}
		}
		for _, t := range tpl.Lines {
			if strings.Contains(t, "{{backend}}") {
				if backend == "" {
					continue
				}
				t = strings.ReplaceAll(t, "{{backend}}", backend)
			}
			l, _ := ParseLine(indent+t, 0)
			l.File = fi
			body = append(body, l)
		}
	}
	doc := e.cfg.Files[fi].Doc
	fb := explode(doc)
	nb := &block{sec: sec, body: body, dirty: true}
	fb.blocks = append(fb.blocks[:local], append([]*block{nb}, fb.blocks[local:]...)...)
	fb.implode(doc)
	return nil
}

func orDefault(s, def string) string {
	if s != "" {
		return s
	}
	return def
}

// defaultPosition es dónde va por defecto una sección nueva dentro del
// archivo: global al principio, defaults justo después de global y el resto al
// final.
func (e *editor) defaultPosition(fi int, typ string) int {
	secs := e.cfg.Files[fi].Doc.Sections
	switch typ {
	case "global":
		return 0
	case "defaults":
		for i, s := range secs {
			if s.Type == "global" {
				return i + 1
			}
		}
		return 0
	}
	return len(secs)
}

func (e *editor) removeSection(op Op) error {
	fi, local, err := e.locate(op.Section)
	if err != nil {
		return err
	}
	doc := e.cfg.Files[fi].Doc
	fb := explode(doc)
	fb.blocks = append(fb.blocks[:local], fb.blocks[local+1:]...)
	fb.implode(doc)
	return nil
}

func (e *editor) moveSection(op Op) error {
	if op.To == nil {
		return fmt.Errorf("falta la posición de destino (to)")
	}
	sf, sl, err := e.locate(op.Section)
	if err != nil {
		return err
	}
	df := sf
	if op.File != nil {
		df = *op.File
		if err := e.checkFile(df); err != nil {
			return err
		}
	}
	// Posición final en la vista unificada, contando sin la sección movida.
	start, count := 0, len(e.cfg.Files[df].Doc.Sections)
	for fi, f := range e.cfg.Files[:df] {
		start += len(f.Doc.Sections)
		if fi == sf {
			start--
		}
	}
	if df == sf {
		count--
	}
	to := *op.To
	if to < start || to > start+count {
		return fmt.Errorf("la posición %d queda fuera del archivo %d (admite %d a %d)", to, df, start, start+count)
	}
	dl := to - start
	if sf == df && dl == sl {
		return nil
	}

	src := e.cfg.Files[sf].Doc
	srcFB := explode(src)
	moved := srcFB.blocks[sl]
	srcFB.blocks = append(srcFB.blocks[:sl], srcFB.blocks[sl+1:]...)
	moved.dirty = true

	if sf == df {
		srcFB.blocks = append(srcFB.blocks[:dl], append([]*block{moved}, srcFB.blocks[dl:]...)...)
		srcFB.implode(src)
		return nil
	}
	srcFB.implode(src)
	dst := e.cfg.Files[df].Doc
	dstFB := explode(dst)
	setFile(moved, df)
	dstFB.blocks = append(dstFB.blocks[:dl], append([]*block{moved}, dstFB.blocks[dl:]...)...)
	dstFB.implode(dst)
	return nil
}

func setFile(b *block, fi int) {
	b.sec.File = fi
	for _, l := range b.lead {
		l.File = fi
	}
	for _, l := range b.body {
		l.File = fi
	}
}

func (e *editor) renameSection(op Op) error {
	s, err := e.section(op.Section)
	if err != nil {
		return err
	}
	info := KB.Sections[s.Type]
	if info.Named == "none" {
		return fmt.Errorf("la sección %s no lleva nombre", s.Type)
	}
	if op.Name == "" && info.Named == "required" {
		return fmt.Errorf("la sección %s necesita un nombre", s.Type)
	}
	if err := validName(op.Name); err != nil {
		return err
	}
	old := s.Name
	if op.Name == old {
		return nil
	}
	if e.nameTaken(s.Type, op.Name, s) {
		return fmt.Errorf("ya existe una sección llamada «%s»", op.Name)
	}

	spans := TokenSpans(s.Text)
	text := s.Text
	switch {
	case op.Name == "" && len(spans) >= 2:
		text = text[:spans[0].End] + text[spans[1].End:]
	case len(spans) >= 2:
		text = text[:spans[1].Start] + QuoteToken(op.Name) + text[spans[1].End:]
	case len(spans) == 1:
		text = text[:spans[0].End] + " " + QuoteToken(op.Name) + text[spans[0].End:]
	}
	_, ns := ParseLine(s.Indent+text, s.Line)
	if ns == nil {
		return fmt.Errorf("no se pudo reescribir la cabecera")
	}
	s.Text, s.Tokens, s.Name, s.Comment = ns.Text, ns.Tokens, ns.Name, ns.Comment

	if op.UpdateRefs && old != "" {
		e.updateRefs(s.Type, old, op.Name)
	}
	return nil
}

// updateRefs reescribe las referencias a una sección renombrada en todos los
// archivos: default_backend y use_backend (si es backend o listen) y los
// argumentos «table <nombre>» de stick, track-sc… (si es un proxy).
func (e *editor) updateRefs(typ, old, name string) {
	isBackend := typ == "backend" || typ == "listen"
	isProxy := isBackend || typ == "frontend"
	for _, sec := range e.all() {
		for _, l := range sec.Lines {
			if l.Kind != KindDirective {
				continue
			}
			var idx []int // posiciones en Args
			switch {
			case isBackend && (l.Keyword == "default_backend" || l.Keyword == "use_backend") && arg(l.Args, 0) == old:
				idx = append(idx, 0)
			}
			if isProxy {
				for j := 0; j+1 < len(l.Args); j++ {
					if l.Args[j] == "table" && l.Args[j+1] == old {
						idx = append(idx, j+1)
					}
				}
			}
			if len(idx) == 0 {
				continue
			}
			spans := TokenSpans(l.Text)
			if len(spans) != len(l.Args)+1 {
				continue
			}
			text := l.Text
			for k := len(idx) - 1; k >= 0; k-- { // de atrás hacia delante para no desplazar posiciones
				sp := spans[idx[k]+1]
				text = text[:sp.Start] + QuoteToken(name) + text[sp.End:]
			}
			if nl, _ := ParseLine(l.Indent+text, l.Line); nl != nil {
				l.Text, l.Args, l.Comment = nl.Text, nl.Args, nl.Comment
			}
		}
	}
}
