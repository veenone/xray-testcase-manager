import ReactMarkdown from "react-markdown";
import type { Components } from "react-markdown";
import remarkGfm from "remark-gfm";

import { BrowserOpenURL } from "../api";

// Markdown renders description / step text as GitHub-flavoured markdown.
//
// react-markdown does NOT render raw embedded HTML by default, so arbitrary
// Jira/Xray content can't inject markup — we deliberately don't add
// rehype-raw. The only customisation is links: inside a WebView a normal
// anchor would navigate the whole app away, so we intercept the click and hand
// the URL to the OS browser via Wails' BrowserOpenURL. stopPropagation keeps
// the click from also toggling an enclosing MarkdownField into edit mode.
const components: Components = {
  a({ href, children, ...props }) {
    return (
      <a
        {...props}
        href={href}
        onClick={(e) => {
          e.preventDefault();
          e.stopPropagation();
          if (href) BrowserOpenURL(href);
        }}
      >
        {children}
      </a>
    );
  },
};

export function Markdown({ children }: { children: string }) {
  return (
    <div className="md-body">
      <ReactMarkdown remarkPlugins={[remarkGfm]} components={components}>
        {children}
      </ReactMarkdown>
    </div>
  );
}
