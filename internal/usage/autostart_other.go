//go:build !darwin && !linux

package usage

import "errors"

const Label = "clashtop"

var errUnsupported = errors.New("后台记录只支持 macOS 和 Linux")

func Supported() bool                              { return false }
func Install(dir, exe string, args []string) error { return errUnsupported }
func Uninstall(dir string) error                   { return errUnsupported }
func Status() (installed, loaded bool, exe string) { return }
