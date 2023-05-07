package collector

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/javascript"
	"golang.org/x/net/html"
)

var ErrRanksNotFound = errors.New("ranks not found")

func CollectElimStats() (any, error) {
	client := &http.Client{}

	req, err := http.NewRequest("GET", "https://ut4stats.com/elim_ranks", nil)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	doc, err := html.Parse(resp.Body)
	if err != nil {
		return nil, err
	}

	// Search the DOM tree for script nodes
	scripts := make([]string, 0, 10)

	stack := []*html.Node{doc}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		if n.Type == html.ElementNode && n.Data == "script" {
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				if c.Type == html.TextNode {
					scripts = append(scripts, c.Data)
				}
			}

			continue
		}

		for c := n.LastChild; c != nil; c = c.PrevSibling {
			stack = append(stack, c)
		}
	}

	// Parse each script with tree-sitter and find the one containing the ranks
	lang := javascript.GetLanguage()
	parser := sitter.NewParser()
	parser.SetLanguage(lang)

	query := `
	(
		(variable_declarator
			name: (identifier) @var-name
			value: (array) @var-value)
		(#eq? @var-name "ranks")
	)
	`

	q, err := sitter.NewQuery([]byte(query), lang)
	if err != nil {
		return nil, err
	}

	// Search for the script containing the ranks variable declaration
	for _, script := range scripts {
		source := []byte(script)

		tree, err := parser.ParseCtx(context.Background(), nil, source)
		if err != nil {
			fmt.Printf("Failed to parse script: %v\n", err)
			continue
		}

		root := tree.RootNode()

		qc := sitter.NewQueryCursor()
		qc.Exec(q, root)

		for {
			m, ok := qc.NextMatch()
			if !ok {
				break
			}

			m = qc.FilterPredicates(m, source)
			for _, c := range m.Captures {
				name := q.CaptureNameForId(c.Index)

				if name == "var-value" {
					ranks, err := buildRanks(c.Node, source)
					if err != nil {
						return nil, err
					}

					return ranks, nil
				}
			}
		}
	}

	return nil, ErrRanksNotFound
}

var ErrUnexpectedType = errors.New("unexpected type")

func buildRanks(node *sitter.Node, source []byte) (any, error) {
	return createObject(node, source)
}

func createObject(node *sitter.Node, source []byte) (any, error) {
	switch node.Type() {
	case "array":
		a := make([]any, 0, 10)

		for i := 0; i < int(node.NamedChildCount()); i++ {
			child := node.NamedChild(i)
			value, err := createObject(child, source)
			if err != nil {
				return nil, err
			}

			a = append(a, value)
		}

		return a, nil

	case "object":
		o := make(map[any]any)

		for i := 0; i < int(node.NamedChildCount()); i++ {
			child := node.NamedChild(i)

			key, err := createObject(child.ChildByFieldName("key"), source)
			if err != nil {
				return nil, err
			}

			value, err := createObject(child.ChildByFieldName("value"), source)
			if err != nil {
				return nil, err
			}

			o[key] = value
		}

		return o, nil

	case "string":
		return node.Content(source), nil

	case "number":
		numeric, err := strconv.ParseFloat(node.Content(source), 64)
		if err != nil {
			return nil, err
		}

		return numeric, err
	}

	return nil, nil
}
