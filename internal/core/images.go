package core

import (
	"fmt"

	"smate/internal/images"
	"smate/internal/runtime"
	"smate/internal/store"
)

type LibraryImage struct {
	Name  string
	Tag   string
	Built bool
	Info  runtime.ImageInfo
}

func ImageNames(s *store.Store) ([]string, error) {
	dir := s.ImagesDir()
	if err := images.Seed(dir); err != nil {
		return nil, err
	}
	return images.List(dir)
}

func Images(s *store.Store) ([]LibraryImage, error) {
	names, err := ImageNames(s)
	if err != nil {
		return nil, err
	}
	var out []LibraryImage
	for _, name := range names {
		tag := images.Tag(name)
		info, built := runtime.InspectImage(tag)
		out = append(out, LibraryImage{Name: name, Tag: tag, Built: built, Info: info})
	}
	return out, nil
}

// Build builds a library image. An unbuilt base is reported by name: docker would
// only say the image cannot be pulled.
func Build(s *store.Store, name string) error {
	dir := s.ImagesDir()
	if err := images.Seed(dir); err != nil {
		return err
	}
	if !images.Exists(dir, name) {
		return fmt.Errorf("%s is not in the image library (%s)", name, dir)
	}
	if base, ok := images.BaseOf(dir, name); ok {
		if _, built := runtime.InspectImage(images.Tag(base)); !built {
			return fmt.Errorf("%s builds on %s, which is not built yet — run: smate build %s",
				name, images.Tag(base), base)
		}
	}
	return runtime.Build(images.Tag(name), s.ImageDir(name))
}

func ResetImages(s *store.Store, name string) ([]string, error) {
	dir := s.ImagesDir()
	if err := images.Seed(dir); err != nil {
		return nil, err
	}
	if name != "" {
		if err := images.Reset(dir, name); err != nil {
			return nil, err
		}
		return []string{name}, nil
	}
	names, err := images.Bundled()
	if err != nil {
		return nil, err
	}
	for _, n := range names {
		if err := images.Reset(dir, n); err != nil {
			return nil, err
		}
	}
	return names, nil
}

// imageRef turns the name from .smate.yml into a docker reference: a library name
// becomes its tag, anything else is passed through untouched.
func imageRef(s *store.Store, name string) (string, error) {
	dir := s.ImagesDir()
	if err := images.Seed(dir); err != nil {
		return "", err
	}
	if !images.Exists(dir, name) {
		return name, nil
	}
	tag := images.Tag(name)
	if _, built := runtime.InspectImage(tag); !built {
		return "", fmt.Errorf("image %s is not built yet — run: smate build %s", tag, name)
	}
	return tag, nil
}
