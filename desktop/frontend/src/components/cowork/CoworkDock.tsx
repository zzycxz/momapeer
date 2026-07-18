export function CoworkDock(_props: {
  cwd?: string;
  maximized: boolean;
  onClose: () => void;
  onToggleMaximized: () => void;
  mode?: "default" | "rag";
  onEntityClick?: (name: string) => void;
  onFileClick?: (path: string) => void;
}) {
  return (
    <aside className="cowork-dock">
      <div>CoworkDock placeholder</div>
    </aside>
  );
}
