package oidc

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"

	"go.yaml.in/yaml/v3"
)

type mappingScope int

const (
	homeSettings    mappingScope = iota // homeassistant:
	packageList                         // homeassistant.packages:
	packageContents                     // top-level integrations in one package
)

var errManagedElsewhere = errors.New("auth_oidc configured in a package")

// Only inspect mappings that can introduce the provider. Other integrations'
// !include and !secret values are opaque.
func usesPackageOIDC(root *os.Root, node *yaml.Node, dir string, scope mappingScope, depth int) (bool, error) {
	err := inspectPackages(root, node, dir, scope, depth)
	if errors.Is(err, errManagedElsewhere) {
		return true, nil
	}
	return false, err
}

func inspectPackages(root *os.Root, node *yaml.Node, dir string, scope mappingScope, depth int) error {
	if depth >= 32 {
		return errors.New("cyclic or deeply nested Home Assistant package configuration")
	}
	switch node.Tag {
	case "!!null":
		return nil
	case "!include", "!include_dir_named", "!include_dir_merge_named":
		name := node.Value
		if strings.HasPrefix(name, "/config/") {
			name = strings.TrimPrefix(name, "/config/")
			dir = "."
		}
		if path.IsAbs(name) {
			return errors.New("package include is outside /config")
		}
		name = path.Join(dir, name)
		if node.Tag == "!include" {
			return inspectPackageFile(root, name, scope, depth+1)
		}
		fileScope := scope
		if node.Tag == "!include_dir_named" {
			if scope != packageList {
				return errors.New("unsupported !include_dir_named outside the Home Assistant packages list")
			}
			fileScope = packageContents
		}
		return fs.WalkDir(root.FS(), name, func(file string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				if file != name && strings.HasPrefix(entry.Name(), ".") {
					return fs.SkipDir
				}
				return nil
			}
			base := entry.Name()
			if strings.HasPrefix(base, ".") || !strings.HasSuffix(base, ".yaml") || base == "secrets.yaml" {
				return nil
			}
			return inspectPackageFile(root, file, fileScope, depth+1)
		})
	}
	if node.Kind == yaml.AliasNode {
		return inspectPackages(root, node.Alias, dir, scope, depth+1)
	}
	if node.Kind != yaml.MappingNode || node.Tag != "!!map" {
		return errors.New("cannot determine auth_oidc ownership from Home Assistant packages")
	}
	for i := 0; i < len(node.Content); i += 2 {
		key, value := node.Content[i], node.Content[i+1]
		if key.Tag != "!!str" {
			return errors.New("cannot determine auth_oidc ownership from merged or dynamic package keys")
		}
		switch scope {
		case homeSettings:
			if key.Value == "packages" {
				if err := inspectPackages(root, value, dir, packageList, depth+1); err != nil {
					return err
				}
			}
		case packageList:
			if err := inspectPackages(root, value, dir, packageContents, depth+1); err != nil {
				return err
			}
		case packageContents:
			if domain(key.Value) == "auth_oidc" {
				return errManagedElsewhere
			}
		}
	}
	return nil
}

func inspectPackageFile(root *os.Root, name string, scope mappingScope, depth int) error {
	info, err := root.Stat(name)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("expected regular YAML package file: %s", name)
	}
	data, err := root.ReadFile(name)
	if err != nil {
		return err
	}
	var doc, extra yaml.Node
	d := yaml.NewDecoder(bytes.NewReader(data))
	if err := d.Decode(&doc); err == io.EOF {
		return nil
	} else if err != nil || len(doc.Content) != 1 || d.Decode(&extra) != io.EOF {
		return fmt.Errorf("cannot parse YAML package: %s", name)
	}
	return inspectPackages(root, doc.Content[0], path.Dir(name), scope, depth)
}
