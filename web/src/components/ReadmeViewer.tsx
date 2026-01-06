import ReactMarkdown from 'react-markdown';
import './ReadmeViewer.css';

interface ReadmeViewerProps {
  content: string;
}

export function ReadmeViewer({ content }: ReadmeViewerProps) {
  return (
    <div className="readme-viewer">
      <div className="readme-header">
        <span className="readme-icon">📖</span>
        <span className="readme-title">README.md</span>
      </div>
      <div className="readme-content">
        <ReactMarkdown>{content}</ReactMarkdown>
      </div>
    </div>
  );
}
