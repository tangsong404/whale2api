package toolcall

import "strings"

func normalizeDSMLToolCallMarkup(text string) (string, bool) {
	if text == "" {
		return "", true
	}
	out := text
	hasAliasLikeMarkup, _ := ContainsToolMarkupSyntaxOutsideIgnored(text)
	if hasAliasLikeMarkup {
		out = rewriteDSMLToolMarkupOutsideIgnored(out)
	}
	if strings.Contains(out, "[[") {
		out = rewriteDoubleBracketRawOutsideIgnored(out)
	}
	return out, true
}

func rewriteDSMLToolMarkupOutsideIgnored(text string) string {
	if text == "" {
		return ""
	}
	lower := strings.ToLower(text)
	var b strings.Builder
	b.Grow(len(text))
	for i := 0; i < len(text); {
		next, advanced, blocked := skipXMLIgnoredSection(text, lower, i)
		if blocked {
			b.WriteString(text[i:])
			break
		}
		if advanced {
			b.WriteString(text[i:next])
			i = next
			continue
		}
		tag, ok := scanToolMarkupTagAt(text, i)
		if !ok {
			b.WriteByte(text[i])
			i++
			continue
		}
		if tag.DSMLLike {
			b.WriteByte('<')
			if tag.Closing {
				b.WriteByte('/')
			}
			b.WriteString(tag.Name)
			if tag.ColonStyle {
				closeStart := tag.End + 1
				for closeStart > tag.NameEnd && text[closeStart-1] == ':' {
					closeStart--
				}
				if closeStart > tag.NameEnd {
					b.WriteString(text[tag.NameEnd:closeStart])
				}
				b.WriteByte('>')
			} else {
				b.WriteString(text[tag.NameEnd : tag.End+1])
				if text[tag.End] != '>' {
					b.WriteByte('>')
				}
			}
			i = tag.End + 1
			continue
		}
		b.WriteString(text[tag.Start : tag.End+1])
		i = tag.End + 1
	}
	return b.String()
}

func rewriteDoubleBracketRawOutsideIgnored(text string) string {
	if text == "" || !strings.Contains(text, "[[") {
		return text
	}
	lower := strings.ToLower(text)
	var b strings.Builder
	b.Grow(len(text) + 16)
	for i := 0; i < len(text); {
		next, advanced, blocked := skipXMLIgnoredSectionOpts(text, lower, i, false)
		if blocked {
			b.WriteString(text[i:])
			break
		}
		if advanced {
			b.WriteString(text[i:next])
			i = next
			continue
		}
		if strings.HasPrefix(text[i:], "[[") {
			end, ok := findDoubleBracketClose(text, i+2)
			if !ok {
				b.WriteString(text[i:])
				break
			}
			inner := text[i+2 : end]
			inner = strings.ReplaceAll(inner, "]] ]]", "]]")
			b.WriteString("<![CDATA[")
			if strings.Contains(inner, "]]>") {
				b.WriteString(strings.ReplaceAll(inner, "]]>", "]]]]><![CDATA[>"))
			} else {
				b.WriteString(inner)
			}
			b.WriteString("]]>")
			i = end + 2
			continue
		}
		b.WriteByte(text[i])
		i++
	}
	return b.String()
}

func findDoubleBracketClose(text string, from int) (int, bool) {
	for i := from; i+1 < len(text); i++ {
		if text[i] == ']' && text[i+1] == ']' {
			// Escaped closer: "]] ]]"
			if i+4 < len(text) && text[i+2] == ' ' && text[i+3] == ']' && text[i+4] == ']' {
				i += 4
				continue
			}
			return i, true
		}
	}
	return -1, false
}
