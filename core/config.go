package core

// HAProxy puede cargar varios archivos (`-f a.cfg -f conf.d/`). Se procesan
// en el orden de carga como si fueran uno solo, con dos reglas:
//   - cada archivo empieza fuera de cualquier sección: una sección no puede
//     repartirse entre archivos;
//   - lo demás sí cruza archivos: la herencia de `defaults`, las referencias a
//     backends, los nombres duplicados…
//
// Config representa ese conjunto. La expansión de directorios (solo los
// `.cfg` no ocultos, en orden lexicográfico) la hace quien lee del disco,
// porque el núcleo no accede al sistema de archivos: así funciona igual en el
// CLI, en `haro serve` y en WASM.

// Source es un archivo de configuración ya leído.
type Source struct {
	Path string `json:"path"`
	Text string `json:"text"`
}

// File es un archivo parseado dentro de un Config.
type File struct {
	Path string `json:"path"`
	Doc  *Doc   `json:"doc"`
}

// Config es una configuración formada por uno o varios archivos, en orden de carga.
type Config struct {
	Files []*File `json:"files"`
}

// NewConfig parsea cada archivo y marca sus líneas y secciones con su índice.
func NewConfig(sources []Source) *Config {
	cfg := &Config{Files: make([]*File, len(sources))}
	for i, src := range sources {
		doc := Parse(src.Text)
		for _, l := range doc.Pre {
			l.File = i
		}
		for _, s := range doc.Sections {
			s.File = i
			for _, l := range s.Lines {
				l.File = i
			}
		}
		cfg.Files[i] = &File{Path: src.Path, Doc: doc}
	}
	return cfg
}

// Merge devuelve una vista unificada en orden de carga. Las secciones y
// líneas son los mismos punteros que en cada archivo, así que editar a
// través de la vista unificada modifica el archivo correspondiente.
func (c *Config) Merge() *Doc {
	m := &Doc{Pre: []*Line{}, Sections: []*Section{}, Files: make([]string, len(c.Files))}
	for i, f := range c.Files {
		m.Files[i] = f.Path
		m.Pre = append(m.Pre, f.Doc.Pre...)
		m.Sections = append(m.Sections, f.Doc.Sections...)
	}
	return m
}

// Serialize devuelve el texto del archivo i.
func (c *Config) Serialize(i int) string {
	return Serialize(c.Files[i].Doc)
}

// AnalyzeConfig analiza una configuración de varios archivos. Las líneas de
// problemas y explicaciones son relativas a su archivo (campo File).
func AnalyzeConfig(sources []Source) Analysis {
	return analyzeDoc(NewConfig(sources).Merge())
}
