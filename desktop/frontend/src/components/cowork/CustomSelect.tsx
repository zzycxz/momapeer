import React, { useState, useRef, useEffect } from "react";
import { ChevronDown, Check } from "lucide-react";

export interface CustomSelectOption {
  value: string;
  label: React.ReactNode;
  icon?: React.ReactNode;
  subtitle?: string;
  indent?: boolean; // 是否是子分类 (缩进展示)
  disabled?: boolean;
}

export interface CustomSelectProps {
  value: string;
  onChange: (val: string) => void;
  options: CustomSelectOption[];
  placeholder?: string;
  icon?: React.ReactNode;
  className?: string;
  style?: React.CSSProperties;
}

export function CustomSelect({
  value,
  onChange,
  options,
  placeholder = "请选择...",
  icon,
  className = "",
  style,
}: CustomSelectProps) {
  const [isOpen, setIsOpen] = useState(false);
  const containerRef = useRef<HTMLDivElement>(null);

  const selectedOption = options.find((o) => o.value === value);

  // 点击外部自动收起浮窗
  useEffect(() => {
    const handleOutsideClick = (e: MouseEvent) => {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        setIsOpen(false);
      }
    };
    if (isOpen) {
      document.addEventListener("mousedown", handleOutsideClick);
    }
    return () => {
      document.removeEventListener("mousedown", handleOutsideClick);
    };
  }, [isOpen]);

  return (
    <div
      ref={containerRef}
      className={`custom-select-wrap ${className}`}
      style={{ position: "relative", width: "100%", ...style }}
    >
      {/* 触发器按钮 (Trigger) */}
      <button
        type="button"
        onClick={() => setIsOpen(!isOpen)}
        style={{
          width: "100%",
          minHeight: 32,
          padding: "5px 10px",
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
          gap: 8,
          borderRadius: 6,
          background: isOpen
            ? "color-mix(in srgb, var(--accent) 8%, var(--bg-elev, #25252d))"
            : "var(--bg-elev, #25252d)",
          border: `1px solid ${isOpen ? "var(--accent, #e67e22)" : "var(--border-soft, rgba(255,255,255,0.1))"}`,
          color: "var(--fg, #e0e0e0)",
          fontSize: "12px",
          fontWeight: 500,
          cursor: "pointer",
          outline: "none",
          transition: "all 0.15s ease",
          boxShadow: isOpen ? "0 0 0 2px color-mix(in srgb, var(--accent) 20%, transparent)" : "none",
          textAlign: "left",
        }}
      >
        <div style={{ display: "flex", alignItems: "center", gap: 6, minWidth: 0, flex: 1 }}>
          {selectedOption?.icon ? (
            <span style={{ color: "var(--accent)", display: "inline-flex" }}>{selectedOption.icon}</span>
          ) : icon ? (
            <span style={{ color: "var(--fg-dim)", display: "inline-flex" }}>{icon}</span>
          ) : null}
          <span style={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
            {selectedOption ? selectedOption.label : placeholder}
          </span>
        </div>
        <ChevronDown
          size={14}
          style={{
            flex: "0 0 auto",
            color: isOpen ? "var(--accent)" : "var(--fg-dim)",
            transform: isOpen ? "rotate(180deg)" : "rotate(0deg)",
            transition: "transform 0.2s cubic-bezier(0.16, 1, 0.3, 1)",
          }}
        />
      </button>

      {/* 悬浮菜单下拉选项 (Popover List) */}
      {isOpen && (
        <div
          style={{
            position: "absolute",
            top: "calc(100% + 4px)",
            left: 0,
            right: 0,
            zIndex: 9999,
            maxHeight: 250,
            overflowY: "auto",
            background: "color-mix(in srgb, var(--bg-elev, #23232b) 95%, #000 5%)",
            backdropFilter: "blur(14px)",
            border: "1px solid var(--border, rgba(255,255,255,0.14))",
            borderRadius: 8,
            padding: "4px",
            boxShadow: "0 12px 32px rgba(0, 0, 0, 0.55), 0 2px 6px rgba(0, 0, 0, 0.3)",
            animation: "customSelectFadeIn 0.15s ease-out",
          }}
        >
          {options.length === 0 ? (
            <div style={{ padding: "12px", textAlign: "center", color: "var(--fg-faint)", fontSize: "11.5px" }}>
              暂无可选项
            </div>
          ) : (
            options.map((opt) => {
              const isSelected = opt.value === value;
              return (
                <div
                  key={opt.value}
                  onClick={() => {
                    if (opt.disabled) return;
                    onChange(opt.value);
                    setIsOpen(false);
                  }}
                  style={{
                    display: "flex",
                    alignItems: "center",
                    justifyContent: "space-between",
                    padding: "6px 8px",
                    paddingLeft: opt.indent ? "20px" : "8px",
                    borderRadius: 5,
                    cursor: opt.disabled ? "not-allowed" : "pointer",
                    fontSize: "12px",
                    color: isSelected ? "var(--fg)" : "var(--fg-dim)",
                    background: isSelected
                      ? "color-mix(in srgb, var(--accent) 15%, transparent)"
                      : "transparent",
                    fontWeight: isSelected ? 600 : 400,
                    transition: "background 0.1s ease, color 0.1s ease",
                    opacity: opt.disabled ? 0.4 : 1,
                  }}
                  onMouseEnter={(e) => {
                    if (!isSelected && !opt.disabled) {
                      e.currentTarget.style.background = "color-mix(in srgb, var(--fg) 8%, transparent)";
                      e.currentTarget.style.color = "var(--fg)";
                    }
                  }}
                  onMouseLeave={(e) => {
                    if (!isSelected) {
                      e.currentTarget.style.background = "transparent";
                      e.currentTarget.style.color = "var(--fg-dim)";
                    }
                  }}
                >
                  <div style={{ display: "flex", alignItems: "center", gap: 6, minWidth: 0 }}>
                    {opt.icon && <span style={{ display: "inline-flex", opacity: 0.85 }}>{opt.icon}</span>}
                    <span style={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                      {opt.label}
                    </span>
                    {opt.subtitle && (
                      <span style={{ fontSize: "10.5px", color: "var(--fg-faint)", marginLeft: 4 }}>
                        {opt.subtitle}
                      </span>
                    )}
                  </div>
                  {isSelected && <Check size={13} style={{ color: "var(--accent)", flex: "0 0 auto", marginLeft: 6 }} />}
                </div>
              );
            })
          )}
        </div>
      )}
    </div>
  );
}
