//go:build !windows && !linux

package screen

import (
	"fmt"
	"image"
)

func Size() (image.Rectangle, error) {
	return image.Rectangle{}, fmt.Errorf("reading the screen isn't supported on this system")
}

func Grab(image.Rectangle) (image.Image, error) {
	return nil, fmt.Errorf("reading the screen isn't supported on this system")
}

func Wayland() bool { return false }
