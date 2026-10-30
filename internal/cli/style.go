package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/fatih/color"
	"github.com/mattn/go-runewidth"
)

// 色の組み合わせ。色を付けるかは出力先ごとに style が決めるので、fatih/color 自身の判定 (標準出力だけを見る) は使わない。
var (
	colorError   = forced(color.New(color.FgRed, color.Bold))
	colorWarn    = forced(color.New(color.FgYellow, color.Bold))
	colorOK      = forced(color.New(color.FgGreen))
	colorHeading = forced(color.New(color.Bold))
)

func forced(c *color.Color) *color.Color {
	c.EnableColor()
	return c
}

// style は、1 つの出力先に色を付けるか。付けないときは、文字をそのまま返す。
type style struct {
	enabled bool
}

func (s style) paint(c *color.Color, text string) string {
	if !s.enabled {
		return text
	}
	return c.Sprint(text)
}

func (env *Env) outStyle() style { return style{enabled: env.OutColor} }
func (env *Env) errStyle() style { return style{enabled: env.ErrColor} }

// warnf は、標準エラーに警告を出す。
func (env *Env) warnf(format string, args ...any) {
	fmt.Fprintf(env.Err, "%s %s\n", env.errStyle().paint(colorWarn, "警告:"), fmt.Sprintf(format, args...))
}

// writeTable は、列を表示の幅でそろえて書く。1 行目は見出しとして太字にする。
func writeTable(w io.Writer, s style, rows [][]string) {
	widths := make([]int, len(rows[0]))
	for _, row := range rows {
		for i, cell := range row {
			widths[i] = max(widths[i], runewidth.StringWidth(cell))
		}
	}
	for n, row := range rows {
		var line strings.Builder
		for i, cell := range row {
			if n == 0 {
				line.WriteString(s.paint(colorHeading, cell))
			} else {
				line.WriteString(cell)
			}
			if i < len(row)-1 {
				line.WriteString(strings.Repeat(" ", widths[i]-runewidth.StringWidth(cell)+2))
			}
		}
		fmt.Fprintln(w, strings.TrimRight(line.String(), " "))
	}
}
