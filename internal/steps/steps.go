// Package steps imports all built-in step packages so their init() registers.
package steps

import (
	_ "github.com/sunshow/siphongear/internal/steps/extract"
	_ "github.com/sunshow/siphongear/internal/steps/fetch"
	_ "github.com/sunshow/siphongear/internal/steps/input"
	_ "github.com/sunshow/siphongear/internal/steps/parse"
	_ "github.com/sunshow/siphongear/internal/steps/script"
	_ "github.com/sunshow/siphongear/internal/steps/session"
	_ "github.com/sunshow/siphongear/internal/steps/transform"
)
