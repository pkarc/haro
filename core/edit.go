package core

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
)

// Edit operations. All the interfaces (the web's visual mode, TUI,
// MCP, assistant) edit through Apply so that the change to the text is
// always minimal: only what the operation needs is touched, and files that
// the operation does not mention are not touched at all.
//
// Section indices are those of the unified view (Config.Merge), the
// same ones used by Issue.Section, LineExplanation.Section and the topology. Line
// indices (LineIndex) are positions within Section.Lines, which
// includes comments and blank lines. Operations are applied in order,
// and each one sees the result of the previous one.

//go:embed knowledge/templates.json
var templatesJSON []byte

// SectionTemplate is the template of a new section. Lines that
// contain {{backend}} are omitted if the configuration has no backend.
type SectionTemplate struct {
	Name  string   `json:"name"`
	Lines []string `json:"lines"`
}

// Templates are the section templates by type, loaded at startup.
var Templates = mustLoadTemplates()

func mustLoadTemplates() map[string]SectionTemplate {
	var t map[string]SectionTemplate
	if err := json.Unmarshal(templatesJSON, &t); err != nil {
		panic(fmt.Sprintf("haro: invalid templates: %v", err))
	}
	return t
}

// Operation names.
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

// Op is an edit operation serializable to JSON. Depending on Op,
// some fields or others are used:
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
// When creating or moving a section, At and To are positions in the unified view.
// File chooses the destination file; without File, add_section infers the file from
// the section occupying that position (or uses the last file, and the first for
// global) and move_section leaves the section in its file.
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

// FileChange is the new text of a file modified by Apply.
type FileChange struct {
	File int    `json:"file"`
	Path string `json:"path"`
	Text string `json:"text"`
}

// ApplyError says which operation failed. If one fails, none is applied.
type ApplyError struct {
	Index int    `json:"index"`
	Op    string `json:"op"`
	Msg   string `json:"message"`
	lang  Lang
}

func (e *ApplyError) Error() string {
	return fmt.Sprintf(e.lang.t("operación %d (%s): %s", "operation %d (%s): %s"), e.Index+1, e.Op, e.Msg)
}

// Apply applies the operations to a copy of cfg and returns the new
// configuration (re-analyzed, with up-to-date line numbers) and the files
// whose text changed. cfg is not modified. If an operation fails none is applied
// and the error is an *ApplyError.
func Apply(cfg *Config, ops ...Op) (*Config, []FileChange, error) {
	return ApplyLang(cfg, LangDefault, ops...)
}

// ApplyLang is Apply with error messages in the requested language.
func ApplyLang(cfg *Config, lang Lang, ops ...Op) (*Config, []FileChange, error) {
	before := make([]Source, len(cfg.Files))
	for i, f := range cfg.Files {
		before[i] = Source{Path: f.Path, Text: Serialize(f.Doc)}
	}
	e := &editor{cfg: NewConfig(before), lang: lang}
	for i, op := range ops {
		if err := e.apply(op); err != nil {
			return nil, nil, &ApplyError{Index: i, Op: op.Op, Msg: err.Error(), lang: lang}
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
	cfg  *Config
	lang Lang
}

// errf builds an error whose message depends on the language.
func errf(lang Lang, es, en string, a ...any) error {
	return fmt.Errorf(lang.t(es, en), a...)
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
	return errf(e.lang, "operación desconocida", "unknown operation")
}

// --- Blocks ---------------------------------------------------------------

// The parser leaves the comments right above a header at the end
// of the previous section. To move or delete sections they must be carried
// along with it, so each file is temporarily decomposed into blocks.

type block struct {
	lead  []*Line // unindented comments attached to the header
	sec   *Section
	body  []*Line // lines of the section, without the next block's comments
	dirty bool    // new or moved section: its separation must be ensured
}

type fileBlocks struct {
	pre    []*Line
	blocks []*block
	endsNL bool // the original file ended with a newline
}

// splitLead splits the end of lines into (rest, comments attached to the
// next header).
func splitLead(lines []*Line) (rest, lead []*Line) {
	k := len(lines)
	for k > 0 && lines[k-1].Kind == KindComment && lines[k-1].Indent == "" {
		k--
	}
	// Full slice expression: a later append must not overwrite the comments.
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

// implode writes the blocks back into the document. It only adds blank lines
// around what is new or moved and keeps the final newline.
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

// --- Locating -----------------------------------------------------------

func (e *editor) total() int {
	n := 0
	for _, f := range e.cfg.Files {
		n += len(f.Doc.Sections)
	}
	return n
}

// locate translates an index of the unified view into (file, position in the file).
func (e *editor) locate(i int) (file, local int, err error) {
	if i < 0 {
		return 0, 0, errf(e.lang, "la sección %d no existe", "section %d does not exist", i)
	}
	for fi, f := range e.cfg.Files {
		if i < len(f.Doc.Sections) {
			return fi, i, nil
		}
		i -= len(f.Doc.Sections)
	}
	return 0, 0, errf(e.lang, "la sección no existe (hay %d)", "the section does not exist (there are %d)", e.total())
}

func (e *editor) section(i int) (*Section, error) {
	fi, local, err := e.locate(i)
	if err != nil {
		return nil, err
	}
	return e.cfg.Files[fi].Doc.Sections[local], nil
}

// fileStart is the unified index of the first section of file fi.
func (e *editor) fileStart(fi int) int {
	n := 0
	for _, f := range e.cfg.Files[:fi] {
		n += len(f.Doc.Sections)
	}
	return n
}

func (e *editor) checkFile(fi int) error {
	if fi < 0 || fi >= len(e.cfg.Files) {
		return errf(e.lang, "el archivo %d no existe (hay %d)", "file %d does not exist (there are %d)", fi, len(e.cfg.Files))
	}
	return nil
}

func (e *editor) all() []*Section { return e.cfg.Merge().Sections }

// --- Name validation ---------------------------------------------------

func validName(name string, lang Lang) error {
	if strings.ContainsAny(name, " \t\r\n#\"'\\") {
		return errf(lang, "el nombre «%s» no puede contener espacios, comillas, # ni \\", "the name “%s” cannot contain spaces, quotes, # or \\", name)
	}
	return nil
}

// nameKeys are the namespaces a section occupies: a listen is both
// frontend and backend, as in the detector.
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

// --- Lines -------------------------------------------------------------

// detectIndent returns the indentation the section uses (or, if it has no
// directives, its file) so new lines match the style.
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

// parseOpLine interprets the text of a directive or comment with the
// indentation it will have. It rejects line breaks, empty lines and section
// headers (which would change the file's structure when rewritten).
func parseOpLine(text, indent string, file int, lang Lang) (*Line, error) {
	if strings.ContainsAny(text, "\r\n") {
		return nil, errf(lang, "el texto debe ser una sola línea", "the text must be a single line")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, errf(lang, "el texto está vacío", "the text is empty")
	}
	l, sec := ParseLine(indent+text, 0)
	if sec != nil {
		return nil, errf(lang, "«%s» es una cabecera de sección, no una directiva; usa add_section", "“%s” is a section header, not a directive; use add_section", text)
	}
	l.File = file
	l.Indent, l.IndentSet = indent, true
	return l, nil
}

// bodyEnd is the position where the section's own content ends: before
// the trailing blank lines and, if another section follows in the file,
// before the comments that belong to its header.
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
		return nil, nil, errf(e.lang, "la línea %d no existe en la sección (tiene %d)", "line %d does not exist in the section (it has %d)", op.LineIndex, len(s.Lines))
	}
	return s, s.Lines[op.LineIndex], nil
}

func (e *editor) addDirective(op Op) error {
	s, err := e.section(op.Section)
	if err != nil {
		return err
	}
	l, err := parseOpLine(op.Text, e.detectIndent(s), s.File, e.lang)
	if err != nil {
		return err
	}
	at := e.bodyEnd(s)
	if op.At != nil {
		if *op.At < 0 || *op.At > len(s.Lines) {
			return errf(e.lang, "la posición %d está fuera de la sección (tiene %d líneas)", "position %d is outside the section (it has %d lines)", *op.At, len(s.Lines))
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
		return errf(e.lang, "la línea %d está en blanco; usa add_directive", "line %d is blank; use add_directive", op.LineIndex)
	}
	indent := old.Indent
	if indent == "" {
		indent = e.detectIndent(s)
	}
	l, err := parseOpLine(op.Text, indent, s.File, e.lang)
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

// moveDirective swaps the line with its non-empty neighbor |Delta| times. This way
// the blank lines that group the section stay where they are. It stops
// at the limits of the section's content.
func (e *editor) moveDirective(op Op) error {
	s, l, err := e.lineAt(op)
	if err != nil {
		return err
	}
	if l.Kind == KindBlank {
		return errf(e.lang, "no se puede mover una línea en blanco", "cannot move a blank line")
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

// --- Sections -----------------------------------------------------------

func (e *editor) addSection(op Op) error {
	info, ok := KB.Sections[op.Type]
	if !ok {
		return errf(e.lang, "tipo de sección desconocido: «%s»", "unknown section type: “%s”", op.Type)
	}
	if len(e.cfg.Files) == 0 {
		return errf(e.lang, "no hay archivos donde añadir la sección", "there are no files to add the section to")
	}
	tpl := Templates[op.Type]
	name := op.Name
	switch info.Named {
	case "none":
		if name != "" {
			return errf(e.lang, "la sección %s no lleva nombre", "the %s section takes no name", op.Type)
		}
	case "required":
		if name == "" {
			name = uniqueName(e, op.Type, orDefault(tpl.Name, op.Type))
		}
	}
	if err := validName(name, e.lang); err != nil {
		return err
	}
	if op.Type == "global" {
		for _, s := range e.all() {
			if s.Type == "global" {
				return errf(e.lang, "ya hay una sección global", "there is already a global section")
			}
		}
	}
	if e.nameTaken(op.Type, name, nil) {
		return errf(e.lang, "ya existe una sección llamada «%s»", "a section named “%s” already exists", name)
	}

	// File and position within the file.
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
				return errf(e.lang, "la posición %d queda fuera del archivo %d", "position %d is outside file %d", *op.At, fi)
			}
			local = *op.At - start
		}
	case op.At != nil:
		at := *op.At
		if at < 0 || at > e.total() {
			return errf(e.lang, "la posición %d no es válida (hay %d secciones)", "position %d is not valid (there are %d sections)", at, e.total())
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

// defaultPosition is where a new section goes by default within the
// file: global at the start, defaults right after global and the rest at the
// end.
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
		return errf(e.lang, "falta la posición de destino (to)", "missing destination position (to)")
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
	// Final position in the unified view, counting without the moved section.
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
		return errf(e.lang, "la posición %d queda fuera del archivo %d (admite %d a %d)", "position %d is outside file %d (allowed %d to %d)", to, df, start, start+count)
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
		return errf(e.lang, "la sección %s no lleva nombre", "the %s section takes no name", s.Type)
	}
	if op.Name == "" && info.Named == "required" {
		return errf(e.lang, "la sección %s necesita un nombre", "the %s section needs a name", s.Type)
	}
	if err := validName(op.Name, e.lang); err != nil {
		return err
	}
	old := s.Name
	if op.Name == old {
		return nil
	}
	if e.nameTaken(s.Type, op.Name, s) {
		return errf(e.lang, "ya existe una sección llamada «%s»", "a section named “%s” already exists", op.Name)
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
		return errf(e.lang, "no se pudo reescribir la cabecera", "could not rewrite the header")
	}
	s.Text, s.Tokens, s.Name, s.Comment = ns.Text, ns.Tokens, ns.Name, ns.Comment

	if op.UpdateRefs && old != "" {
		e.updateRefs(s.Type, old, op.Name)
	}
	return nil
}

// updateRefs rewrites the references to a renamed section in all
// files: default_backend and use_backend (if it is a backend or listen) and the
// "table <name>" arguments of stick, track-sc... (if it is a proxy).
func (e *editor) updateRefs(typ, old, name string) {
	isBackend := typ == "backend" || typ == "listen"
	isProxy := isBackend || typ == "frontend"
	for _, sec := range e.all() {
		for _, l := range sec.Lines {
			if l.Kind != KindDirective {
				continue
			}
			var idx []int // positions in Args
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
			for k := len(idx) - 1; k >= 0; k-- { // back to front so as not to shift positions
				sp := spans[idx[k]+1]
				text = text[:sp.Start] + QuoteToken(name) + text[sp.End:]
			}
			if nl, _ := ParseLine(l.Indent+text, l.Line); nl != nil {
				l.Text, l.Args, l.Comment = nl.Text, nl.Args, nl.Comment
			}
		}
	}
}
