import MarkdownIt from 'markdown-it'

// Model output is untrusted. Raw HTML and remotely loaded images stay disabled.
const markdown = new MarkdownIt({ html: false, linkify: false, breaks: true }).disable('image')

export function renderAssistantAnswer(answer) {
  return markdown.render(String(answer || ''))
}
