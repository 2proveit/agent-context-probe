import { useState } from 'react';
import { Copy, Check, FileCode, Download, Maximize2, X } from 'lucide-react';
import { tokenizeCodeLine } from '../utils/toolResultRendering';

interface CodeViewerProps {
  code: string;
  fileName?: string;
  language?: string;
}

export function CodeViewer({ code, fileName, language }: CodeViewerProps) {
  const [copied, setCopied] = useState(false);
  const [isFullscreen, setIsFullscreen] = useState(false);

  // Determine language from file extension
  const getLanguageFromFileName = (filename?: string): string => {
    if (!filename) return 'text';
    
    const extension = filename.split('.').pop()?.toLowerCase();
    const languageMap: Record<string, string> = {
      'js': 'javascript',
      'jsx': 'javascript',
      'ts': 'typescript',
      'tsx': 'typescript',
      'py': 'python',
      'rb': 'ruby',
      'go': 'go',
      'rs': 'rust',
      'java': 'java',
      'cpp': 'cpp',
      'c': 'c',
      'h': 'c',
      'hpp': 'cpp',
      'cs': 'csharp',
      'php': 'php',
      'swift': 'swift',
      'kt': 'kotlin',
      'scala': 'scala',
      'r': 'r',
      'sh': 'bash',
      'bash': 'bash',
      'zsh': 'bash',
      'fish': 'bash',
      'ps1': 'powershell',
      'sql': 'sql',
      'html': 'html',
      'htm': 'html',
      'xml': 'xml',
      'css': 'css',
      'scss': 'scss',
      'sass': 'sass',
      'less': 'less',
      'json': 'json',
      'yaml': 'yaml',
      'yml': 'yaml',
      'toml': 'toml',
      'md': 'markdown',
      'mdx': 'markdown',
      'tex': 'latex',
      'dockerfile': 'dockerfile',
      'makefile': 'makefile',
      'cmake': 'cmake',
      'gradle': 'gradle',
      'maven': 'xml',
      'vim': 'vim',
      'lua': 'lua',
      'dart': 'dart',
      'elixir': 'elixir',
      'elm': 'elm',
      'erlang': 'erlang',
      'haskell': 'haskell',
      'julia': 'julia',
      'nim': 'nim',
      'perl': 'perl',
      'ocaml': 'ocaml',
      'clj': 'clojure',
      'cljs': 'clojure',
      'cljc': 'clojure'
    };
    
    return languageMap[extension || ''] || 'text';
  };

  const detectedLanguage = language || getLanguageFromFileName(fileName);

  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(code);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch (error) {
      console.error('Failed to copy to clipboard:', error);
    }
  };

  const handleDownload = () => {
    const blob = new Blob([code], { type: 'text/plain' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = fileName || 'code.txt';
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);
  };

  const lines = code.split('\n');
  const lineCount = lines.length;

  const CodeDisplay = ({ inModal = false }: { inModal?: boolean }) => (
    <div className={`rounded-lg border border-slate-200 bg-white overflow-hidden shadow-sm ${inModal ? '' : 'max-h-[600px]'}`}>
      {/* Header */}
      <div className="px-4 py-2 bg-slate-50 border-b border-slate-200 flex items-center justify-between">
        <div className="flex items-center space-x-3">
          <FileCode className="w-4 h-4 text-blue-600" />
          <span className="text-sm text-slate-700 font-mono">
            {fileName || 'Untitled'}
          </span>
          <span className="text-xs text-slate-600 bg-slate-200/70 px-2 py-1 rounded">
            {detectedLanguage}
          </span>
          <span className="text-xs text-slate-500">
            {lineCount} lines
          </span>
        </div>
        <div className="flex items-center space-x-2">
          <button
            onClick={handleDownload}
            className="p-1.5 text-slate-500 hover:text-blue-700 hover:bg-blue-50 rounded transition-colors"
            title="Download file"
          >
            <Download className="w-4 h-4" />
          </button>
          {!inModal && (
            <button
              onClick={() => setIsFullscreen(true)}
              className="p-1.5 text-slate-500 hover:text-blue-700 hover:bg-blue-50 rounded transition-colors"
              title="View fullscreen"
            >
              <Maximize2 className="w-4 h-4" />
            </button>
          )}
          <button
            onClick={handleCopy}
            className="p-1.5 text-slate-500 hover:text-blue-700 hover:bg-blue-50 rounded transition-colors"
            title="Copy code"
          >
            {copied ? (
              <Check className="w-4 h-4 text-emerald-600" />
            ) : (
              <Copy className="w-4 h-4" />
            )}
          </button>
        </div>
      </div>

      {/* Code content */}
      <div className={`overflow-auto ${inModal ? 'max-h-[80vh]' : 'max-h-[500px]'}`}>
        <table className="w-full text-sm font-mono">
          <tbody>
            {lines.map((line, idx) => (
              <tr key={idx} className="hover:bg-blue-50/60">
                <td className="px-4 py-0.5 text-right text-slate-400 bg-slate-50/70 border-r border-slate-100 select-none w-12 align-top">
                  {idx + 1}
                </td>
                <td className="px-4 py-0.5 whitespace-pre text-slate-700">
                  <span>
                    {tokenizeCodeLine(line).map((token, tokenIndex) => (
                      token.className ? (
                        <span key={tokenIndex} className={token.className}>{token.text}</span>
                      ) : token.text
                    ))}
                  </span>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );

  return (
    <>
      <CodeDisplay />

      {/* Fullscreen Modal */}
      {isFullscreen && (
        <div
          className="fixed inset-0 z-50 bg-black bg-opacity-90 flex items-center justify-center p-4"
          onClick={() => setIsFullscreen(false)}
        >
          <div className="relative max-w-[90vw] w-full max-h-[90vh]" onClick={(e) => e.stopPropagation()}>
            <button
              onClick={() => setIsFullscreen(false)}
              className="absolute -top-10 right-0 p-2 text-white hover:text-gray-300 transition-colors"
              title="Close"
            >
              <X className="w-6 h-6" />
            </button>
            <CodeDisplay inModal />
          </div>
        </div>
      )}
    </>
  );
}
