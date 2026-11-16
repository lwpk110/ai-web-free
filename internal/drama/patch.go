package drama

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

type PatchOperation struct {
	Op    string          `json:"op"`
	Path  string          `json:"path"`
	From  string          `json:"from,omitempty"`
	Value json.RawMessage `json:"value,omitempty"`
}

func decodePointer(pointer string) ([]string, error) {
	if pointer == "" {
		return nil, nil
	}
	if !strings.HasPrefix(pointer, "/") {
		return nil, fmt.Errorf("invalid JSON pointer %q: must start with /", pointer)
	}
	parts := strings.Split(pointer[1:], "/")
	for i, p := range parts {
		p = strings.ReplaceAll(p, "~1", "/")
		p = strings.ReplaceAll(p, "~0", "~")
		parts[i] = p
	}
	return parts, nil
}

func getAt(doc interface{}, tokens []string) (interface{}, error) {
	cur := doc
	for _, tok := range tokens {
		switch node := cur.(type) {
		case map[string]interface{}:
			val, ok := node[tok]
			if !ok {
				return nil, fmt.Errorf("path token %q not found", tok)
			}
			cur = val
		case []interface{}:
			idx, err := strconv.Atoi(tok)
			if err != nil || idx < 0 || idx >= len(node) {
				return nil, fmt.Errorf("array index %q out of range", tok)
			}
			cur = node[idx]
		default:
			return nil, fmt.Errorf("cannot traverse %T with token %q", cur, tok)
		}
	}
	return cur, nil
}

func setAt(doc interface{}, tokens []string, val interface{}, mustExist bool) (interface{}, error) {
	if len(tokens) == 0 {
		return val, nil
	}
	parentTokens := tokens[:len(tokens)-1]
	last := tokens[len(tokens)-1]
	parent, err := getAt(doc, parentTokens)
	if err != nil {
		return nil, err
	}
	switch p := parent.(type) {
	case map[string]interface{}:
		if mustExist {
			if _, ok := p[last]; !ok {
				return nil, fmt.Errorf("replace target %q does not exist", last)
			}
		}
		p[last] = val
		return doc, nil
	case []interface{}:
		if last == "-" && !mustExist {
			newArr := append(p, val)
			return replaceContainer(doc, parentTokens, newArr)
		}
		idx, err := strconv.Atoi(last)
		if err != nil {
			return nil, fmt.Errorf("invalid array index %q", last)
		}
		if mustExist {
			if idx < 0 || idx >= len(p) {
				return nil, fmt.Errorf("array index %d out of range", idx)
			}
			p[idx] = val
			return doc, nil
		}
		if idx < 0 || idx > len(p) {
			return nil, fmt.Errorf("array insert index %d out of range", idx)
		}
		newArr := make([]interface{}, 0, len(p)+1)
		newArr = append(newArr, p[:idx]...)
		newArr = append(newArr, val)
		newArr = append(newArr, p[idx:]...)
		return replaceContainer(doc, parentTokens, newArr)
	default:
		return nil, fmt.Errorf("parent is not container (%T)", parent)
	}
}

func removeAt(doc interface{}, tokens []string) (interface{}, interface{}, error) {
	if len(tokens) == 0 {
		return nil, nil, errors.New("cannot remove root document")
	}
	parentTokens := tokens[:len(tokens)-1]
	last := tokens[len(tokens)-1]
	parent, err := getAt(doc, parentTokens)
	if err != nil {
		return nil, nil, err
	}
	switch p := parent.(type) {
	case map[string]interface{}:
		old, ok := p[last]
		if !ok {
			return nil, nil, fmt.Errorf("remove target %q not found", last)
		}
		delete(p, last)
		return doc, old, nil
	case []interface{}:
		idx, err := strconv.Atoi(last)
		if err != nil || idx < 0 || idx >= len(p) {
			return nil, nil, fmt.Errorf("array remove index %q out of range", last)
		}
		old := p[idx]
		newArr := make([]interface{}, 0, len(p)-1)
		newArr = append(newArr, p[:idx]...)
		newArr = append(newArr, p[idx+1:]...)
		updated, err := replaceContainer(doc, parentTokens, newArr)
		return updated, old, err
	default:
		return nil, nil, fmt.Errorf("parent is not container (%T)", parent)
	}
}

func replaceContainer(doc interface{}, tokens []string, newVal interface{}) (interface{}, error) {
	if len(tokens) == 0 {
		return newVal, nil
	}
	return setAt(doc, tokens, newVal, true)
}

// ApplyJSONPatch 将 RFC 6902 JSON Patch 应用到 DramaProject，并自动递增 revision 及重算 stale 状态。
func ApplyJSONPatch(proj *DramaProject, ops []PatchOperation) (*DramaProject, error) {
	if proj == nil {
		return nil, errors.New("project is nil")
	}
	raw, err := json.Marshal(proj)
	if err != nil {
		return nil, err
	}
	var doc interface{}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}

	for i, op := range ops {
		tokens, err := decodePointer(op.Path)
		if err != nil {
			return nil, fmt.Errorf("op[%d]: %w", i, err)
		}
		switch strings.ToLower(strings.TrimSpace(op.Op)) {
		case "add":
			var val interface{}
			if err := json.Unmarshal(op.Value, &val); err != nil {
				return nil, fmt.Errorf("op[%d] invalid value: %w", i, err)
			}
			doc, err = setAt(doc, tokens, val, false)
			if err != nil {
				return nil, fmt.Errorf("op[%d] add: %w", i, err)
			}
		case "replace":
			var val interface{}
			if err := json.Unmarshal(op.Value, &val); err != nil {
				return nil, fmt.Errorf("op[%d] invalid value: %w", i, err)
			}
			doc, err = setAt(doc, tokens, val, true)
			if err != nil {
				return nil, fmt.Errorf("op[%d] replace: %w", i, err)
			}
		case "remove":
			doc, _, err = removeAt(doc, tokens)
			if err != nil {
				return nil, fmt.Errorf("op[%d] remove: %w", i, err)
			}
		case "copy":
			fromTokens, err := decodePointer(op.From)
			if err != nil {
				return nil, fmt.Errorf("op[%d] from: %w", i, err)
			}
			val, err := getAt(doc, fromTokens)
			if err != nil {
				return nil, fmt.Errorf("op[%d] copy: %w", i, err)
			}
			doc, err = setAt(doc, tokens, val, false)
			if err != nil {
				return nil, fmt.Errorf("op[%d] copy: %w", i, err)
			}
		case "move":
			fromTokens, err := decodePointer(op.From)
			if err != nil {
				return nil, fmt.Errorf("op[%d] from: %w", i, err)
			}
			var val interface{}
			doc, val, err = removeAt(doc, fromTokens)
			if err != nil {
				return nil, fmt.Errorf("op[%d] move: %w", i, err)
			}
			doc, err = setAt(doc, tokens, val, false)
			if err != nil {
				return nil, fmt.Errorf("op[%d] move: %w", i, err)
			}
		case "test":
			var expected interface{}
			if err := json.Unmarshal(op.Value, &expected); err != nil {
				return nil, fmt.Errorf("op[%d] invalid test value: %w", i, err)
			}
			actual, err := getAt(doc, tokens)
			if err != nil {
				return nil, fmt.Errorf("op[%d] test: %w", i, err)
			}
			if !reflect.DeepEqual(actual, expected) {
				return nil, fmt.Errorf("op[%d] test failed at %s", i, op.Path)
			}
		default:
			return nil, fmt.Errorf("op[%d] unsupported op %q", i, op.Op)
		}
	}

	patchedRaw, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	var updated DramaProject
	if err := json.Unmarshal(patchedRaw, &updated); err != nil {
		return nil, fmt.Errorf("patched document violates DramaProject schema: %w", err)
	}
	updated.ID = proj.ID
	updated.SchemaVersion = CurrentSchemaVersion
	updated.Revision = proj.Revision + 1
	updated.EnsureMaps()
	SyncNodeHashesAndStates(&updated, false)
	return &updated, nil
}
