// Comando haro: lee, entiende y valida configuraciones de HAProxy.
package main

import (
	"fmt"
	"os"
)

// version se sobrescribe al compilar: -ldflags "-X main.version=v0.1.0".
var version = "dev"

const usage = `haro — readout para configuraciones de HAProxy

Uso:
  haro lint    [archivo]        Detecta problemas (código de salida 1 si hay errores)
  haro explain [archivo]        Explica cada sección y directiva en lenguaje humano
  haro diff    <antes> <después> Compara dos configuraciones
  haro serve   [opciones]       Abre la interfaz web local
  haro version                  Muestra la versión

Sin archivo, lint y explain leen /etc/haproxy/haproxy.cfg; usa "-" para stdin.
Ejecuta "haro <comando> -h" para ver las opciones de cada comando.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Print(usage)
		return
	}
	cmd, args := os.Args[1], os.Args[2:]
	var err error
	switch cmd {
	case "lint":
		err = runLint(args)
	case "explain":
		err = runExplain(args)
	case "diff":
		err = runDiff(args)
	case "serve":
		err = runServe(args)
	case "version", "--version", "-v":
		fmt.Println("haro", version)
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "haro: comando desconocido %q\n\n%s", cmd, usage)
		os.Exit(2)
	}
	if err != nil {
		if e, ok := err.(exitError); ok {
			os.Exit(int(e))
		}
		fmt.Fprintln(os.Stderr, "haro:", err)
		os.Exit(1)
	}
}

// exitError permite terminar con un código concreto sin imprimir nada más.
type exitError int

func (e exitError) Error() string { return fmt.Sprintf("exit %d", int(e)) }
