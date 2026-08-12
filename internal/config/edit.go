package config

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"

	"gopkg.in/yaml.v3"
)

// The configuration file is edited through the YAML node tree rather than by
// decoding into Config and marshalling back.
//
// Marshalling would lose two things. The file is commented throughout, and the
// comments are the documentation of every key. Decode also copies the defaults
// block into every server, so a round trip would write out fields the user
// deliberately left to defaults.
//
// The node tree keeps both: comments travel on the nodes, and untouched
// sections are re-emitted exactly as they were parsed.

// yamlIndent matches the indentation the example file uses, so an edit does not
// reflow the parts it did not touch.
const yamlIndent = 2

// ErrNoServersKey reports a file this package cannot edit, because it has no
// servers list to append to.
var ErrNoServersKey = errors.New("ayar dosyasında servers listesi bulunamadı")

// AddServer appends a server to the servers list and returns the new file.
//
// Only the fields the caller filled in are written. An empty field is left out
// so the server keeps inheriting it from the defaults block, which is what
// makes the file readable after several servers have been added.
func AddServer(source []byte, srv Server) ([]byte, error) {
	doc, err := parseDocument(source)
	if err != nil {
		return nil, err
	}

	servers, err := serversNode(doc)
	if err != nil {
		return nil, err
	}

	entry, err := serverNode(srv)
	if err != nil {
		return nil, err
	}

	servers.Content = append(servers.Content, entry)

	// An emptied list carries flow style, because an empty block sequence
	// cannot be written. Now that it has an item, block style is readable
	// again.
	servers.Style = 0

	return encodeDocument(doc)
}

// ClearServers empties the servers list, keeping every other section and its
// comments.
//
// This is what turns the shipped example into a starting point for a machine
// that has no configuration yet: the sample servers go, the documentation of
// every other key stays, and no second copy of the template has to exist.
func ClearServers(source []byte) ([]byte, error) {
	doc, err := parseDocument(source)
	if err != nil {
		return nil, err
	}

	servers, err := serversNode(doc)
	if err != nil {
		return nil, err
	}

	servers.Content = nil

	// An empty sequence has to be emitted in flow style, because a block
	// sequence with no items is not valid YAML.
	servers.Style = yaml.FlowStyle

	return encodeDocument(doc)
}

func parseDocument(source []byte) (*yaml.Node, error) {
	var doc yaml.Node

	if err := yaml.Unmarshal(source, &doc); err != nil {
		return nil, fmt.Errorf("ayar dosyası çözümlenemedi: %w", err)
	}

	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return nil, ErrNoServersKey
	}

	return &doc, nil
}

// serversNode finds the sequence the servers key holds.
func serversNode(doc *yaml.Node) (*yaml.Node, error) {
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, ErrNoServersKey
	}

	// A mapping stores key and value as consecutive entries.
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != "servers" {
			continue
		}

		value := root.Content[i+1]
		if value.Kind != yaml.SequenceNode {
			return nil, fmt.Errorf("%w: servers bir liste değil", ErrNoServersKey)
		}

		return value, nil
	}

	return nil, ErrNoServersKey
}

// serverNode builds the mapping written for one server, in the field order the
// example file uses.
func serverNode(srv Server) (*yaml.Node, error) {
	if srv.Host == "" {
		return nil, errors.New("host alanı zorunlu")
	}

	node := &yaml.Node{Kind: yaml.MappingNode}

	add := func(key, value string) {
		if value == "" {
			return
		}

		node.Content = append(node.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: key},
			&yaml.Node{Kind: yaml.ScalarNode, Value: value})
	}

	add("name", srv.Name)
	add("host", srv.Host)
	add("user", srv.User)

	// A zero port is not written at all, so ssh keeps resolving it itself.
	if srv.Port != 0 {
		add("port", strconv.Itoa(srv.Port))
	}

	add("records_file", srv.RecordsFile)
	add("main_config", srv.MainConfig)

	if srv.Sudo != nil {
		node.Content = append(node.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: "sudo"},
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: strconv.FormatBool(*srv.Sudo)})
	}

	return node, nil
}

func encodeDocument(doc *yaml.Node) ([]byte, error) {
	var buf bytes.Buffer

	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(yamlIndent)

	if err := enc.Encode(doc); err != nil {
		return nil, fmt.Errorf("ayar dosyası yazılamadı: %w", err)
	}

	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("ayar dosyası yazılamadı: %w", err)
	}

	return spaceTopLevelSections(buf.Bytes()), nil
}

// spaceTopLevelSections puts a blank line back before every top-level key.
//
// YAML has no concept of a blank line, so the encoder drops the ones that
// separate the sections of this file. Without them the file grows denser with
// every edit, and this file is the documentation of its own keys.
//
// Inserting only where the line is not already blank keeps the pass
// idempotent, so editing an already-spaced file changes nothing.
func spaceTopLevelSections(data []byte) []byte {
	lines := bytes.Split(data, []byte("\n"))
	out := make([][]byte, 0, len(lines))

	for i, line := range lines {
		if startsTopLevelSection(lines, i) && len(out) > 0 && len(bytes.TrimSpace(out[len(out)-1])) > 0 {
			out = append(out, nil)
		}

		out = append(out, line)
	}

	return bytes.Join(out, []byte("\n"))
}

// startsTopLevelSection reports whether line i opens a top-level key, counting
// the comment block written above it as part of it.
func startsTopLevelSection(lines [][]byte, i int) bool {
	if !isTopLevel(lines[i]) {
		return false
	}

	if bytes.HasPrefix(lines[i], []byte("#")) {
		// Only the first line of a comment block opens the section; the rest
		// of the block continues it.
		return i == 0 || !bytes.HasPrefix(lines[i-1], []byte("#"))
	}

	// A key directly under its own comment block was already spaced when that
	// block opened.
	return i == 0 || !bytes.HasPrefix(lines[i-1], []byte("#"))
}

// isTopLevel reports whether a line sits at column zero, which in this file
// means a section key or the comment introducing one.
func isTopLevel(line []byte) bool {
	trimmed := bytes.TrimSpace(line)
	if len(trimmed) == 0 {
		return false
	}

	return !bytes.HasPrefix(line, []byte(" ")) && !bytes.HasPrefix(line, []byte("-"))
}
