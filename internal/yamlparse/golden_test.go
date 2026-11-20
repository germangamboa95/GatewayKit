package yamlparse

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden files")

// tree renders one node per line, indented, with kind and position.
func tree(n *Node) string {
	var b strings.Builder
	writeTree(&b, n, "", 0)
	return b.String()
}

func writeTree(b *strings.Builder, n *Node, label string, depth int) {
	b.WriteString(strings.Repeat("  ", depth))
	b.WriteString(label)
	pos := fmt.Sprintf("@%d:%d", n.Line, n.Col)
	switch n.Kind {
	case Null:
		b.WriteString("null " + pos + "\n")
	case Scalar:
		q := "plain"
		if n.Quoted {
			q = "quoted"
		}
		fmt.Fprintf(b, "%s %s %s\n", strconv.Quote(n.Value), q, pos)
	case Seq:
		fmt.Fprintf(b, "seq(%d) %s\n", len(n.Items), pos)
		for i, it := range n.Items {
			writeTree(b, it, fmt.Sprintf("- [%d] ", i), depth+1)
		}
	case Map:
		fmt.Fprintf(b, "map(%d) %s\n", len(n.Keys), pos)
		for _, k := range n.Keys {
			writeTree(b, n.Map[k], strconv.Quote(k)+": ", depth+1)
		}
	}
}

func TestGoldenConfigs(t *testing.T) {
	for _, name := range []string{"gateway", "variant-a", "variant-b", "variant-c"} {
		t.Run(name, func(t *testing.T) {
			src, err := os.ReadFile(filepath.Join("..", "..", "configs", name+".yaml"))
			if err != nil {
				t.Fatal(err)
			}
			n, err := ParseYAML(src)
			if err != nil {
				t.Fatalf("%s.yaml: %v", name, err)
			}
			got := tree(n)
			golden := filepath.Join("testdata", name+".golden")
			if *update {
				if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatal(err)
			}
			if got != string(want) {
				t.Errorf("%s.yaml tree differs from %s; rerun with -update and review\n%s", name, golden, got)
			}
		})
	}
}

func TestSampleSpotChecks(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "configs", "gateway.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	root := mustParse(t, string(src))
	if got := strings.Join(root.Keys, ","); got != "gateway,routes" {
		t.Fatalf("root keys %s", got)
	}
	gw := root.Map["gateway"]
	if got := strings.Join(gw.Keys, ","); got != "port,global_timeout,global_rate_limit" {
		t.Errorf("gateway keys %s", got)
	}
	if p := gw.Map["port"]; p.Value != "8080" || p.Quoted || p.Line != 3 || p.Col != 9 {
		t.Errorf("port %+v", p)
	}
	routes := root.Map["routes"]
	if routes.Kind != Seq || len(routes.Items) != 5 {
		t.Fatalf("routes %v len %d", routes.Kind, len(routes.Items))
	}
	legacy := routes.Items[3]
	add := legacy.Map["request_transform"].Map["headers"].Map["add"]
	if v := add.Map["X-Request-Start"]; v.Value != "$request_time" || !v.Quoted {
		t.Errorf("X-Request-Start %+v", v)
	}
	mapping := legacy.Map["request_transform"].Map["body"].Map["mapping"]
	if got := strings.Join(mapping.Keys, ","); got != "user.id,user.name,meta.source,meta.timestamp" {
		t.Errorf("mapping keys %s", got)
	}
	on := routes.Items[1].Map["upstream"].Map["retry"].Map["on"]
	if on.Kind != Seq || len(on.Items) != 3 || on.Items[0].Value != "502" || on.Items[0].Quoted {
		t.Errorf("retry.on %s", dump(on, true))
	}
}
