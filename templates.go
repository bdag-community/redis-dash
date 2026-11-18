package main

const indexTemplate = `
{{define "index"}}
<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta name="theme-color" content="#08080d">
  <title>{{.Title}}</title>
  <link rel="preconnect" href="https://fonts.googleapis.com">
  <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
  <link href="https://fonts.googleapis.com/css2?family=Inter:wght@400;600;700&family=JetBrains+Mono:wght@400;600&display=swap" rel="stylesheet">
  <script src="/static/htmx-2.0.4.min.js"></script>
  <style>
    :root {
      color-scheme: dark;
      --bg: #09090b;
      --surface: #0f0f13;
      --panel: #14141a;
      --panel-strong: #181820;
      --text: #fafafa;
      --muted: #a1a1aa;
      --line: #27272f;
      --accent: #2bd4a3;
      --accent-dark: #a9f5df;
      --accent-soft: rgba(43, 212, 163, 0.12);
      --brand: #a856fb;
      --brand-dark: #d8bbff;
      --brand-soft: rgba(168, 86, 251, 0.16);
      --warning: #f5c45e;
      --danger: #fb7185;
      --blue: #60a5fa;
      --orange: #fb923c;
      --shadow: 0 14px 40px rgba(0, 0, 0, 0.22);
      --nav-tab-height: 42px;
      font-family: Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      min-height: 100vh;
      background: var(--bg);
      color: var(--text);
      overflow-x: hidden;
    }
    .app-shell {
      min-height: 100vh;
      display: grid;
      grid-template-columns: 272px minmax(0, 1fr);
    }
    .sidebar {
      position: sticky;
      top: 0;
      height: 100vh;
      display: flex;
      flex-direction: column;
      border-right: 1px solid var(--line);
      background: rgba(20, 20, 26, 0.76);
      backdrop-filter: blur(18px);
      z-index: 20;
    }
    .sidebar-header {
      display: flex;
      align-items: center;
      gap: 12px;
      min-height: 70px;
      padding: 0 18px;
      border-bottom: 1px solid var(--line);
    }
    .brand-tile {
      width: 42px;
      height: 42px;
      border: 1px solid rgba(168, 86, 251, 0.38);
      border-radius: 8px;
      display: grid;
      place-items: center;
      background: var(--panel-strong);
      text-decoration: none;
    }
    .brand-tile img {
      width: 34px;
      height: 34px;
      object-fit: contain;
    }
    .status-pip {
      width: 9px;
      height: 9px;
      border-radius: 999px;
      background: var(--accent);
      box-shadow: 0 0 0 4px rgba(43, 212, 163, 0.14), 0 0 18px rgba(43, 212, 163, 0.44);
    }
    .sidebar-title {
      margin: 0;
      font-size: 1.06rem;
      line-height: 1.1;
      font-weight: 800;
    }
    .sidebar-subtitle {
      margin: 3px 0 0;
      color: var(--muted);
      font-size: 0.66rem;
      font-weight: 800;
      letter-spacing: 0.12em;
      text-transform: uppercase;
    }
    .sidebar-nav {
      flex: 1;
      min-height: 0;
      overflow: auto;
      padding: 14px 10px;
    }
    .nav-label {
      margin: 8px 10px 10px;
      color: var(--muted);
      font-size: 0.66rem;
      font-weight: 800;
      letter-spacing: 0.1em;
      text-transform: uppercase;
    }
    .sidebar-footer {
      padding: 12px;
      border-top: 1px solid var(--line);
    }
    .system-card {
      border: 1px solid rgba(43, 212, 163, 0.24);
      border-radius: 8px;
      background: rgba(43, 212, 163, 0.08);
      padding: 12px;
    }
    .system-card strong {
      display: flex;
      align-items: center;
      gap: 8px;
      color: var(--accent-dark);
      font-size: 0.78rem;
    }
    .system-card span {
      display: block;
      margin-top: 5px;
      color: var(--muted);
      font-size: 0.7rem;
    }
    .content-shell {
      min-width: 0;
      display: flex;
      flex-direction: column;
      min-height: 100vh;
    }
    .desktop-header {
      position: sticky;
      top: 0;
      z-index: 10;
      display: flex;
      align-items: center;
      justify-content: space-between;
      min-height: 70px;
      padding: 0 26px;
      border-bottom: 1px solid var(--line);
      background: rgba(9, 9, 11, 0.86);
      backdrop-filter: blur(18px);
    }
    .desktop-title {
      margin: 0;
      font-size: 1.02rem;
      font-weight: 800;
    }
    .desktop-subtitle {
      margin: 4px 0 0;
      color: var(--muted);
      font-size: 0.76rem;
    }
    .shell {
      width: min(1440px, 100%);
      padding: 22px 26px 46px;
      min-height: 0;
    }
    .topbar {
      display: flex;
      align-items: flex-start;
      justify-content: space-between;
      gap: 20px;
      margin-bottom: var(--nav-tab-height);
    }
    .brand-lockup {
      display: grid;
      grid-template-columns: minmax(180px, 260px) minmax(0, 1fr);
      gap: 18px 22px;
      width: 100%;
      align-items: center;
      justify-content: between;
      min-width: 0;
    }
    .brand-mark {
      display: inline-flex;
      align-items: center;
      justify-content: flex-start;
      text-decoration: none;
    }
    .brand-mark img {
      width: min(260px, 100%);
      height: auto;
      object-fit: contain;
    }
    .brand-copy {
      justify-self: end;
      min-width: 0;
      text-align: right;
    }
    .eyebrow {
      margin: 0 0 6px;
      color: var(--brand-dark);
      font-size: 0.82rem;
      font-weight: 700;
      letter-spacing: 0.08em;
      text-transform: uppercase;
    }
    h1 {
      margin: 0;
      font-size: clamp(1.8rem, 4vw, 2.8rem);
      line-height: 0.95;
    }
    .dashboard-nav {
      display: grid;
      gap: 6px;
    }
    .nav-tab {
      min-height: var(--nav-tab-height);
      width: 100%;
      border: 1px solid transparent;
      border-radius: 8px;
      background: transparent;
      color: var(--text);
      cursor: pointer;
      display: flex;
      align-items: center;
      justify-content: flex-start;
      gap: 10px;
      padding: 0 12px;
      font-size: 0.88rem;
      font-weight: 700;
      white-space: nowrap;
      transition: border-color 120ms ease, background 120ms ease, color 120ms ease, box-shadow 120ms ease;
    }
    .nav-tab::before {
      content: attr(data-icon);
      width: 24px;
      height: 24px;
      border-radius: 7px;
      display: grid;
      place-items: center;
      border: 1px solid var(--line);
      color: var(--muted);
      font-size: 0.72rem;
      font-weight: 800;
    }
    .nav-tab:hover {
      border-color: var(--brand);
      color: #fff;
      background: rgba(168, 86, 251, 0.08);
    }
    .nav-tab.active {
      background: rgba(168, 86, 251, 0.18);
      border-color: rgba(168, 86, 251, 0.34);
      color: #fff;
      box-shadow: inset 0 0 0 1px rgba(168, 86, 251, 0.18);
    }
    .nav-tab.active::before {
      border-color: rgba(168, 86, 251, 0.54);
      color: var(--brand-dark);
      background: rgba(168, 86, 251, 0.16);
    }
    .dashboard-main {
      display: grid;
      gap: 18px;
      min-height: 0;
      flex: 1;
    }
    .page-header {
      display: flex;
      align-items: flex-end;
      justify-content: space-between;
      gap: 16px;
      min-width: 0;
    }
    .page-title {
      margin: 0;
      font-size: 1.42rem;
      line-height: 1.12;
      font-weight: 800;
    }
    .page-description {
      margin: 6px 0 0;
      color: var(--muted);
      font-size: 0.86rem;
    }
    .content-grid {
      display: grid;
      grid-template-columns: minmax(0, 1.12fr) minmax(320px, 0.88fr);
      gap: 16px;
      align-items: start;
    }
    .content-grid > *,
    .stack-lg,
    .stack-md {
      min-width: 0;
    }
    #tab-content:not(.log-panel) {
      display: grid;
      gap: 16px;
    }
    .panel {
      border: 1px solid var(--line);
      border-radius: 8px;
      background: rgba(28, 28, 34, 0.9);
      box-shadow: var(--shadow);
      padding: 18px;
      min-width: 0;
    }
    .panel-feature {
      border-color: rgba(168, 86, 251, 0.28);
      background: rgba(24, 24, 32, 0.92);
    }
    .section-title {
      margin: 0 0 8px;
      font-size: 1.05rem;
      font-weight: 700;
    }
    .muted { color: var(--muted); }
    .text-soft { color: var(--brand-dark); }
    .center-stack {
      display: flex;
      flex-direction: column;
      align-items: center;
      gap: 12px;
      text-align: center;
    }
    .metric-grid {
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(190px, 1fr));
      justify-content: center;
      gap: 10px;
      padding: 0;
    }
    .metric-card {
      border: 1px solid var(--line);
      border-radius: 8px;
      background: #353531;
      padding: 12px;
      min-width: 0;
    }
    .metric-label {
      font-size: 0.75rem;
      font-weight: 700;
      letter-spacing: 0.06em;
      text-transform: uppercase;
      color: var(--muted);
      text-align: left;
      width: 100%;
    }
    .metric-value {
      margin-top: 8px;
      text-align: right;
      font-size: 1.04rem;
      font-weight: 700;
      word-break: break-word;
    }
    .node-status-grid, .pool-status-grid {
      grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
    }
    .overview-heading {
      margin: 18px 0 12px;
      text-align: center;
      font-size: clamp(1.8rem, 3vw, 2.8rem);
      line-height: 1.05;
      font-weight: 800;
    }
    .overview-section {
      display: grid;
      gap: 12px;
    }
    .overview-card-title {
      margin: 0 0 12px;
      text-align: center;
      font-size: 1.08rem;
      font-weight: 800;
    }
    .node-status-log-grid {
      grid-template-columns: repeat(auto-fit, minmax(160px, 1fr));
    }
    .node-status-log-grid .metric-card {
      min-height: 74px;
    }
    .pool-status-grid .metric-card-pause-reason {
      grid-column: 1 / -1;
    }
    .pool-status-grid .metric-card-endpoint {
      grid-column: span 2;
    }
    .overview-metrics-grid {
      grid-template-columns: repeat(4, minmax(0, 1fr));
    }
    .overview-metrics-grid .metric-card-pause-reason,
    .overview-metrics-grid .metric-card-endpoint {
      grid-column: auto;
    }
    .compact-metrics-grid {
      grid-template-columns: repeat(10, minmax(0, 1fr));
      gap: 8px;
      overflow-x: auto;
      padding-bottom: 2px;
    }
    .compact-metrics-grid .metric-card {
      min-width: 0;
      min-height: 54px;
      padding: 7px 8px;
      display: grid;
      grid-template-rows: 18px 1fr;
      align-items: stretch;
    }
    .compact-metrics-grid .metric-label {
      font-size: 0.58rem;
      line-height: 1.05;
      letter-spacing: 0.03em;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }
    .compact-metrics-grid .metric-value {
      align-self: end;
      margin-top: 0;
      text-align: left;
      font-size: 0.82rem;
      line-height: 1.08;
      overflow: hidden;
      text-overflow: ellipsis;
    }
    .compact-metrics-grid .metric-card-endpoint {
      grid-column: span 2;
    }
    .metric-card-nowrap .metric-value {
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }
    .global-metrics-panel { padding: 12px; }
    .global-metrics-grid {
      grid-template-columns: repeat(5, minmax(112px, 1fr));
      gap: 8px;
      padding: 0;
      align-items: stretch;
    }
    .global-metrics-grid .metric-card {
      min-height: 58px;
      padding: 8px 10px;
      display: grid;
      grid-template-rows: 24px 1fr;
      align-content: stretch;
    }
    .global-metrics-grid .metric-label {
      font-size: 0.62rem;
      line-height: 1.15;
      letter-spacing: 0.04em;
      display: flex;
      align-items: flex-start;
    }
    .global-metrics-grid .metric-value {
      margin-top: 0;
      font-size: 0.92rem;
      line-height: 1.15;
      display: flex;
      align-items: flex-end;
      justify-content: flex-end;
      min-width: 0;
    }
    .chain-dag-metrics {
      grid-template-columns: repeat(8, minmax(0, 1fr));
      gap: 8px;
      overflow-x: auto;
      padding-bottom: 2px;
    }
    .chain-dag-metrics .metric-card {
      min-width: 0;
      min-height: 54px;
      padding: 7px 8px;
      display: grid;
      grid-template-rows: 18px 1fr;
    }
    .chain-dag-metrics .metric-label {
      font-size: 0.58rem;
      line-height: 1.05;
      letter-spacing: 0.03em;
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
    }
    .chain-dag-metrics .metric-value {
      margin-top: 0;
      font-size: 0.82rem;
      line-height: 1.08;
      text-align: left;
      align-self: end;
      overflow: hidden;
      text-overflow: ellipsis;
    }
    .chain-dag-metrics .chain-dag-state {
      width: 100%;
      min-width: 0;
      min-height: 54px;
      padding: 7px 8px;
      font-size: 0.82rem;
      white-space: nowrap;
    }
    .chain-dag-heading-row {
      display: flex;
      align-items: baseline;
      justify-content: space-between;
      gap: 16px;
      min-width: 0;
    }
    .chain-dag-heading-warning {
      margin-left: auto;
      max-width: min(58vw, 720px);
      color: #fde68a;
      font-size: 0.78rem;
      line-height: 1.25;
      text-align: right;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }
    .state-badge {
      display: inline-flex;
      align-items: center;
      justify-content: center;
      min-height: 3.5rem;
      border-radius: 8px;
      border: 1px solid var(--line);
      padding: 0.75rem 1rem;
      font-size: 1.35rem;
      font-weight: 700;
    }
    .state-ok { border-color: rgba(43, 212, 163, 0.45); background: var(--accent-soft); color: var(--accent-dark); }
    .state-warn { border-color: rgba(245, 196, 94, 0.45); background: rgba(245, 196, 94, 0.12); color: #fde68a; }
    .state-down { border-color: rgba(251, 113, 133, 0.45); background: rgba(251, 113, 133, 0.12); color: #fecdd3; }
    .state-neutral { border-color: var(--line); background: var(--panel); color: var(--text); }
    .status-grid {
      display: flex;
      flex-wrap: wrap;
      justify-content: center;
      gap: 8px;
      margin: 14px 0;
    }
    .status-chip {
      display: flex;
      align-items: center;
      justify-content: center;
      min-height: 48px;
      width: auto;
      min-width: 128px;
      border-radius: 8px;
      border: 1px solid var(--line);
      background: rgba(19, 19, 27, 0.72);
      padding: 0.75rem 0.9rem;
      font-size: 0.88rem;
      font-weight: 700;
      color: var(--muted);
      opacity: 0.72;
    }
    .status-chip.active {
      border-color: rgba(168, 86, 251, 0.45);
      background: linear-gradient(135deg, rgba(168, 86, 251, 0.24), rgba(43, 212, 163, 0.08));
      color: #fff;
      opacity: 1;
      box-shadow: 0 0 0 1px rgba(168, 86, 251, 0.18), 0 16px 40px rgba(0, 0, 0, 0.24);
    }
    .api-error {
      border: 1px solid rgba(251, 113, 133, 0.45);
      border-radius: 8px;
      background: rgba(251, 113, 133, 0.12);
      padding: 12px 14px;
      color: #fecdd3;
      font-size: 0.92rem;
    }
    .data-table {
      width: max-content;
      min-width: 100%;
      border-collapse: collapse;
      font-size: 0.92rem;
    }
    .data-table th {
      border-bottom: 1px solid var(--line);
      padding: 10px 12px;
      text-align: left;
      font-size: 0.75rem;
      font-weight: 700;
      letter-spacing: 0.06em;
      text-transform: uppercase;
      color: var(--muted);
      white-space: nowrap;
    }
    .data-table th.has-tooltip {
      position: relative;
      cursor: help;
      text-decoration: underline dotted rgba(161, 161, 170, 0.72);
      text-underline-offset: 4px;
    }
    .data-table th.has-tooltip::before,
    .data-table th.has-tooltip::after {
      opacity: 0;
      pointer-events: none;
      position: absolute;
      transition: opacity 0.12s ease, transform 0.12s ease, visibility 0.12s ease;
      visibility: hidden;
    }
    .data-table th.has-tooltip::before {
      border: 5px solid transparent;
      border-bottom-color: rgba(63, 63, 70, 0.98);
      content: "";
      left: 18px;
      top: calc(100% + 2px);
      transform: translateY(-4px);
      z-index: 21;
    }
    .data-table th.has-tooltip::after {
      background: rgba(24, 24, 33, 0.98);
      border: 1px solid rgba(63, 63, 70, 0.98);
      border-radius: 8px;
      box-shadow: 0 16px 36px rgba(0, 0, 0, 0.38);
      color: #f4f4f5;
      content: attr(data-tooltip);
      font-size: 0.76rem;
      font-weight: 600;
      left: 8px;
      letter-spacing: 0;
      line-height: 1.35;
      max-width: min(360px, 72vw);
      padding: 9px 10px;
      text-transform: none;
      top: calc(100% + 11px);
      transform: translateY(-4px);
      white-space: normal;
      width: max-content;
      z-index: 20;
    }
    .data-table th.has-tooltip:hover::before,
    .data-table th.has-tooltip:hover::after,
    .data-table th.has-tooltip:focus-visible::before,
    .data-table th.has-tooltip:focus-visible::after {
      opacity: 1;
      transform: translateY(0);
      visibility: visible;
    }
    .data-table td {
      border-bottom: 1px solid rgba(45, 41, 56, 0.72);
      padding: 10px 12px;
      vertical-align: top;
      white-space: nowrap;
    }
    .data-table .num { text-align: right; }
    .peer-table-scroll {
      width: 100%;
      height: min(560px, 62vh);
      min-height: 280px;
      margin-top: 12px;
      overflow: auto;
      border: 1px solid var(--line);
      border-radius: 8px;
    }
    .peer-table-scroll .data-table {
      min-width: max(100%, 1440px);
    }
    .peer-table-scroll .data-table th {
      position: sticky;
      top: 0;
      z-index: 1;
      background: var(--panel);
    }
    .peer-table-scroll .data-table th.has-tooltip {
      z-index: 3;
    }
    .peer-table-scroll .hash-cell {
      width: calc(20ch + 24px);
      min-width: calc(20ch + 24px);
      max-width: calc(20ch + 24px);
    }
    .peer-table-scroll td.hash-cell {
      font-family: "JetBrains Mono", ui-monospace, SFMono-Regular, Menlo, monospace;
    }
    .peer-table-scroll .hash-cell-text {
      display: block;
      max-width: 20ch;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
      width: 20ch;
    }
    .asic-detail-row[hidden] { display: none; }
    .asic-detail-row td {
      white-space: normal;
      background: rgba(14, 14, 22, 0.72);
    }
    .asic-detail-panel {
      display: grid;
      gap: 12px;
      min-width: min(980px, calc(100vw - 56px));
    }
    .asic-detail-grid {
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(210px, 1fr));
      gap: 10px;
    }
    .asic-detail-item {
      border: 1px solid rgba(45, 41, 56, 0.86);
      border-radius: 8px;
      background: rgba(8, 8, 13, 0.55);
      padding: 9px 10px;
      min-width: 0;
    }
    .asic-detail-label {
      color: var(--muted);
      font-size: 0.68rem;
      font-weight: 700;
      letter-spacing: 0.05em;
      text-transform: uppercase;
    }
    .asic-detail-value {
      margin-top: 4px;
      color: var(--text);
      overflow-wrap: anywhere;
    }
    .asic-row-action {
      display: inline-flex;
      align-items: center;
      gap: 8px;
    }
    .ghost-button.compact {
      min-height: 30px;
      padding: 0 10px;
      font-size: 0.76rem;
    }
    .ghost-button:disabled {
      cursor: not-allowed;
      opacity: 0.45;
    }
    .row-card {
      display: grid;
      grid-template-columns: minmax(0, 1fr) 120px 120px;
      gap: 12px;
      align-items: center;
      border: 1px solid var(--line);
      border-radius: 8px;
      background: var(--panel);
      padding: 12px 14px;
      font-size: 0.92rem;
    }
    .row-card-head {
      display: grid;
      grid-template-columns: minmax(0, 1fr) 120px 120px;
      gap: 12px;
      padding: 0 14px 8px;
      font-size: 0.75rem;
      font-weight: 700;
      letter-spacing: 0.06em;
      text-transform: uppercase;
      color: var(--muted);
    }
    .range-button {
      min-height: 36px;
      border: 1px solid var(--line);
      border-radius: 8px;
      background: var(--panel);
      color: var(--text);
      padding: 0 12px;
      font-size: 0.82rem;
      font-weight: 600;
      cursor: pointer;
    }
    .range-button:hover:not(:disabled) { border-color: var(--brand); }
    .range-button.active {
      background: var(--brand);
      border-color: var(--brand);
      color: #fff;
    }
    .range-button:disabled {
      cursor: not-allowed;
      opacity: 0.45;
    }
    .range-row, .toolbar-row {
      display: flex;
      flex-wrap: wrap;
      gap: 8px;
      align-items: center;
    }
    .toolbar-row { justify-content: space-between; gap: 12px; }
    .producer-trend-toolbar {
      display: grid;
      grid-template-columns: auto auto minmax(0, 1fr);
      gap: 12px;
      align-items: center;
    }
    .producer-trend-toolbar .section-title { white-space: nowrap; }
    .producer-trend-metrics { justify-content: flex-start; }
    .producer-trend-ranges { justify-content: flex-end; }
    .chart-wrap { height: 320px; min-height: 320px; }
    .chain-dag-wrap {
      height: clamp(392px, 45vh, 532px);
      min-height: 392px;
      overflow-x: auto;
      overflow-y: hidden;
      scrollbar-color: rgba(96, 165, 250, 0.42) rgba(20, 20, 26, 0.9);
    }
    .chart-legend {
      display: flex;
      flex-wrap: wrap;
      gap: 8px 14px;
      margin-top: 12px;
      color: var(--muted);
      font-size: 12px;
    }
    .legend-key, .pool-key {
      display: inline-flex;
      align-items: center;
      gap: 7px;
      min-width: 0;
      border: 1px solid transparent;
      border-radius: 6px;
      padding: 4px 7px;
    }
    .legend-key::before, .pool-key::before {
      content: "";
      width: 10px;
      height: 10px;
      flex: 0 0 10px;
      border-radius: 999px;
      background: var(--key-color, var(--accent));
      box-shadow: 0 0 12px var(--key-color, var(--accent));
    }
    .local-pool-highlight {
      border-color: rgba(43, 212, 163, 0.55);
      background: rgba(43, 212, 163, 0.12);
      color: var(--accent-dark);
    }
    .local-pool-row {
      outline: 1px solid rgba(43, 212, 163, 0.55);
      outline-offset: -2px;
      background: rgba(43, 212, 163, 0.08);
    }
    .ghost-button {
      min-height: 36px;
      border: 1px solid rgba(168, 86, 251, 0.42);
      border-radius: 8px;
      background: rgba(19, 19, 27, 0.74);
      color: var(--brand-dark);
      padding: 0 12px;
      font-size: 0.82rem;
      font-weight: 600;
      cursor: pointer;
    }
    .ghost-button:hover { border-color: var(--brand); color: #fff; }
    .field {
      display: grid;
      gap: 6px;
      min-width: 0;
    }
    .field label, .check-field {
      color: var(--muted);
      font-size: 0.72rem;
      font-weight: 700;
      text-transform: uppercase;
      letter-spacing: 0.06em;
    }
    .field input, .field select {
      width: 100%;
      min-height: 40px;
      border: 1px solid var(--line);
      border-radius: 8px;
      background: rgba(8, 8, 13, 0.84);
      color: var(--text);
      padding: 0 10px;
      font: inherit;
    }
    .form-grid {
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
      gap: 12px;
    }
    .check-field {
      min-height: 40px;
      display: flex;
      align-items: center;
      gap: 8px;
      text-transform: none;
      letter-spacing: 0;
    }
    .action-result {
      border: 1px solid var(--line);
      border-radius: 8px;
      background: rgba(19, 19, 27, 0.74);
      padding: 14px;
      margin-bottom: 16px;
    }
    .action-result.ok { border-color: rgba(43, 212, 163, 0.45); }
    .action-result.warn { border-color: rgba(245, 196, 94, 0.55); }
    .action-result.down { border-color: rgba(251, 113, 133, 0.55); }
    .log-pane {
      flex: 1 1 auto;
      min-height: 0;
      overflow: auto;
      white-space: pre-wrap;
      overflow-wrap: anywhere;
      border: 1px solid var(--line);
      border-radius: 8px;
      background: #171421;
      padding: 12px;
      font-family: "JetBrains Mono", ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
      font-size: 0.78rem;
      line-height: 1.55;
      color: #e8fff7;
    }
    .log-panel { display: flex; min-height: 0; flex: 1; flex-direction: column; }
    .log-grid {
      display: grid;
      grid-template-columns: repeat(2, minmax(0, 1fr));
      gap: 16px;
      min-height: min(720px, calc(100vh - 150px));
    }
    .log-tabs {
      display: flex;
      gap: 8px;
      flex-wrap: wrap;
      margin-bottom: 12px;
    }
    .log-tabs .range-button {
      min-width: 96px;
    }
    .log-shell {
      min-height: min(760px, calc(100vh - 190px));
    }
    .warning-list {
      margin: 0;
      padding-left: 20px;
      color: #fde68a;
    }
    .warning-list li + li { margin-top: 8px; }
    .stack-lg { display: grid; gap: 18px; }
    .stack-md { display: grid; gap: 12px; }
    @media (max-width: 860px) {
      .app-shell { display: block; }
      .sidebar {
        position: sticky;
        height: auto;
        border-right: 0;
        border-bottom: 1px solid var(--line);
      }
      .sidebar-header { min-height: 62px; }
      .sidebar-nav {
        padding: 8px;
        overflow-x: auto;
      }
      .nav-label, .sidebar-footer { display: none; }
      .dashboard-nav {
        grid-auto-flow: column;
        grid-auto-columns: max-content;
        overflow-x: auto;
      }
      .nav-tab {
        width: auto;
        min-width: max-content;
      }
      .desktop-header { display: none; }
      .shell { padding: 16px 14px 36px; }
      .content-grid, .log-grid { grid-template-columns: 1fr; }
      .status-steps {
        display: grid;
        grid-template-columns: repeat(2, minmax(0, 1fr));
        justify-content: stretch;
      }
      .status-chip {
        width: auto;
        min-width: 0;
      }
      .toolbar-row, .row-card, .row-card-head, .producer-trend-toolbar { grid-template-columns: 1fr; display: grid; }
      .producer-trend-ranges { justify-content: flex-start; }
      .overview-metrics-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
      .global-metrics-grid { grid-template-columns: repeat(2, minmax(112px, 1fr)); }
      .chain-dag-metrics { grid-template-columns: repeat(8, minmax(92px, 1fr)); }
      .compact-metrics-grid { grid-template-columns: repeat(10, minmax(92px, 1fr)); }
      .chain-dag-heading-row { align-items: flex-start; }
      .chain-dag-heading-warning { max-width: 55vw; }
      .chain-dag-wrap { height: 350px; min-height: 350px; }
    }
  </style>
</head>
<body>
  <div class="app-shell">
    <aside class="sidebar">
      <div class="sidebar-header">
        <a class="brand-tile" href="/" aria-label="BDAG Community">
          <img src="/static/bdag-community-logo.svg" alt="BDAG Community" />
        </a>
        <div>
          <h1 class="sidebar-title">BDAG Sentinel</h1>
          <p class="sidebar-subtitle">X30 / X100 mining</p>
        </div>
      </div>
      <nav class="sidebar-nav">
        <div class="nav-label">Navigation</div>
        <div class="dashboard-nav">
          <button class="nav-tab active" data-icon="O" hx-get="/tabs/overview" hx-target="#tab-content" hx-swap="outerHTML">Overview</button>
          <button class="nav-tab" data-icon="N" hx-get="/tabs/peers" hx-target="#tab-content" hx-swap="outerHTML">Peers</button>
          <button class="nav-tab" data-icon="!" hx-get="/tabs/warnings" hx-target="#tab-content" hx-swap="outerHTML">Warnings</button>
          <button class="nav-tab" data-icon="A" hx-get="/tabs/asic-fleet" hx-target="#tab-content" hx-swap="outerHTML">ASIC Fleet</button>
          <button class="nav-tab" data-icon="S" hx-get="/tabs/stratum-runtime" hx-target="#tab-content" hx-swap="outerHTML">Stratum Runtime</button>
          <button class="nav-tab" data-icon="P" hx-get="/tabs/pool-management" hx-target="#tab-content" hx-swap="outerHTML">Pool Management</button>
          <button class="nav-tab" data-icon="D" hx-get="/tabs/chain-dag" hx-target="#tab-content" hx-swap="outerHTML">Chain DAG</button>
          <button class="nav-tab" data-icon="$" hx-get="/tabs/pool-earnings" hx-target="#tab-content" hx-swap="outerHTML">Earnings</button>
          <button class="nav-tab" data-icon="G" hx-get="/tabs/global" hx-target="#tab-content" hx-swap="outerHTML">Global</button>
          <button class="nav-tab" data-icon="L" hx-get="/tabs/logs" hx-target="#tab-content" hx-swap="outerHTML">Logs</button>
        </div>
      </nav>
      <div class="sidebar-footer">
        <div class="system-card">
          <strong><span class="status-pip"></span> Local stack online</strong>
          {{with .StackReleaseTag}}<span>{{.}}</span>{{end}}
          <span>Live mining monitor</span>
        </div>
      </div>
    </aside>
    <div class="content-shell">
      <header class="desktop-header">
        <div>
          <h2 class="desktop-title">Mining Operations Dashboard</h2>
          <p class="desktop-subtitle">BlockDAG pool operations, chain safety, and ASIC control</p>
        </div>
      </header>
      <div class="shell">
        <main class="dashboard-main">
          {{template "overview" .Tab}}
        </main>
      </div>
    </div>
  </div>
  <script>
    window.redrawProducerTrend = function () {
      requestAnimationFrame(() => {
        requestAnimationFrame(() => {
          try {
            if (window.drawProducerTrend) window.drawProducerTrend();
          } catch (error) {
            console.error("producer trend redraw failed", error);
          }
        });
      });
    };
    document.body.addEventListener("htmx:afterSwap", (event) => {
      const path = event.detail.xhr?.responseURL || "";
      if (path.includes("/tabs/")) {
        document.querySelectorAll(".nav-tab").forEach((button) => button.classList.remove("active"));
        const active = Array.from(document.querySelectorAll(".nav-tab")).find((button) => path.includes(button.getAttribute("hx-get")));
        if (active) active.classList.add("active");
      }
      if (event.detail.elt.matches?.(".log-pane")) {
        event.detail.elt.scrollTop = event.detail.elt.scrollHeight;
      }
      event.detail.elt.querySelectorAll?.(".log-pane").forEach((pane) => { pane.scrollTop = pane.scrollHeight; });
    });
    document.body.addEventListener("click", async (event) => {
      const logTab = event.target.closest?.("[data-log-tab]");
      if (logTab) {
        event.preventDefault();
        document.querySelectorAll("[data-log-tab]").forEach((button) => button.classList.remove("active"));
        logTab.classList.add("active");
        const panel = document.querySelector("[data-active-log-panel]");
        const title = panel?.querySelector("[data-active-log-title]");
        const pane = panel?.querySelector(".log-pane");
        if (panel) panel.dataset.contentEndpoint = logTab.dataset.logEndpoint || "";
        if (title) title.textContent = logTab.dataset.logTitle || logTab.textContent || "Logs";
        if (pane) pane.textContent = "Loading...";
        void updateLogsView();
        return;
      }
      const button = event.target.closest?.("[data-copy-logs]");
      if (!button) return;
      event.preventDefault();
      event.stopPropagation();
      const panel = button.closest("section");
      const pane = panel?.querySelector(".log-pane");
      if (!pane) return;
      const text = pane.textContent || "";
      const original = button.textContent;
      const copyFallback = () => {
        const textarea = document.createElement("textarea");
        textarea.value = text;
        textarea.setAttribute("readonly", "");
        textarea.style.position = "fixed";
        textarea.style.left = "-9999px";
        textarea.style.top = "0";
        document.body.appendChild(textarea);
        textarea.focus();
        textarea.select();
        const copied = document.execCommand("copy");
        textarea.remove();
        if (!copied) throw new Error("copy command returned false");
      };
      try {
        if (navigator.clipboard?.writeText && window.isSecureContext) {
          await navigator.clipboard.writeText(text);
        } else {
          copyFallback();
        }
        button.textContent = "Copied";
      } catch (error) {
        try {
          copyFallback();
          button.textContent = "Copied";
        } catch (fallbackError) {
          console.error("copy logs failed", error, fallbackError);
          button.textContent = "Copy failed";
        }
      }
      window.setTimeout(() => { button.textContent = original; }, 1500);
    });
    document.body.addEventListener("click", (event) => {
      const button = event.target.closest?.("[data-asic-detail-toggle]");
      if (!button) return;
      event.preventDefault();
      const target = document.getElementById(button.getAttribute("data-asic-detail-target") || "");
      if (!target) return;
      const opening = target.hasAttribute("hidden");
      document.querySelectorAll(".asic-detail-row:not([hidden])").forEach((row) => {
        if (row !== target) row.setAttribute("hidden", "");
      });
      document.querySelectorAll("[data-asic-detail-toggle][aria-expanded='true']").forEach((openButton) => {
        if (openButton !== button) openButton.setAttribute("aria-expanded", "false");
      });
      if (opening) {
        target.removeAttribute("hidden");
        button.setAttribute("aria-expanded", "true");
      } else {
        target.setAttribute("hidden", "");
        button.setAttribute("aria-expanded", "false");
      }
    });
    document.body.addEventListener("htmx:afterSettle", () => {
      window.redrawProducerTrend();
      if (window.refreshLiveTab) window.refreshLiveTab();
    });

    const liveUpdatePending = {status: false, logs: false, poolEarnings: false, global: false, stratumRuntime: false, chainDAG: false};
    function activeLiveTab() {
      return document.getElementById("tab-content")?.dataset.liveTab || "";
    }
    async function liveJSON(url) {
      const response = await fetch(url, {headers: {Accept: "application/json"}, cache: "no-store"});
      if (!response.ok) throw new Error(url + " returned " + response.status);
      return response.json();
    }
    function escapeHTML(value) {
      return String(value ?? "").replace(/[&<>"']/g, (ch) => ({
        "&": "&amp;",
        "<": "&lt;",
        ">": "&gt;",
        "\"": "&quot;",
        "'": "&#39;",
      }[ch]));
    }
    function safeText(value, fallback = "unknown") {
      const text = String(value ?? "").trim();
      return text || fallback;
    }
    function safeColor(value) {
      const color = String(value || "").trim();
      return /^#[0-9a-fA-F]{6}$/.test(color) ? color : "#2bd4a3";
    }
    function setHTML(selector, html) {
      const el = document.querySelector(selector);
      if (el) el.innerHTML = html;
    }
    function setText(selector, text) {
      const el = document.querySelector(selector);
      if (el) el.textContent = safeText(text, "");
    }
    function metricCardClass(label) {
      const text = String(label || "");
      let cls = "metric-card";
      if (text === "Pause Reason") cls += " metric-card-pause-reason metric-card-nowrap";
      if (text === "Pool Endpoint") cls += " metric-card-endpoint metric-card-nowrap";
      return cls;
    }
    function metricCardsHTML(items) {
      const rows = Array.isArray(items) ? items : [];
      if (!rows.length) {
        return '<div class="metric-card"><div class="metric-label">State</div><div class="metric-value">warming up</div></div>';
      }
      return rows.map((item) => {
        return '<div class="' + metricCardClass(item.label) + '"><div class="metric-label">' + escapeHTML(item.label) + '</div><div class="metric-value">' + escapeHTML(safeText(item.value)) + '</div></div>';
      }).join("");
    }
    function stateToneClass(tone) {
      if (tone === "ok") return "state-badge state-ok";
      if (tone === "warn") return "state-badge state-warn";
      if (tone === "down") return "state-badge state-down";
      return "state-badge state-neutral";
    }
    function chainDAGMetricCardsHTML(payload) {
      const state = safeText(payload && payload.state, "Warming");
      const tone = payload && payload.state_tone;
      return '<div class="' + stateToneClass(tone) + ' chain-dag-state">' + escapeHTML(state) + '</div>' + metricCardsHTML(payload && payload.evidence);
    }
    function chainDAGWarningsHTML(warnings) {
      const rows = Array.isArray(warnings) ? warnings.filter(Boolean) : [];
      return rows.map((item) => escapeHTML(item)).join(" · ");
    }
    function statusStepsHTML(steps) {
      const rows = Array.isArray(steps) ? steps : [];
      if (!rows.length) return '<div class="status-chip active">Unknown</div>';
      return rows.map((step) => {
        return '<div class="status-chip' + (step.active ? " active" : "") + '">' + escapeHTML(step.label) + '</div>';
      }).join("");
    }
    function evidenceGroupsHTML(groups) {
      const rows = Array.isArray(groups) ? groups : [];
      return rows.map((group) => {
        return '<section class="panel panel-feature center-stack"><h2 class="section-title">' + escapeHTML(group.title) + '</h2><div style="width: 100%; max-width: 920px;"><div class="metric-grid">' + metricCardsHTML(group.items) + '</div></div></section>';
      }).join("");
    }
    function asicDetailItem(label, value) {
      return '<div class="asic-detail-item"><div class="asic-detail-label">' + escapeHTML(label) + '</div><div class="asic-detail-value">' + escapeHTML(safeText(value, "unknown")) + '</div></div>';
    }
    function asicPerformanceRowsHTML(rows) {
      const data = Array.isArray(rows) ? rows : [];
      if (!data.length) return '<tr><td class="muted" colspan="9" style="padding: 18px 12px;">No ASIC performance rows yet.</td></tr>';
      return data.map((row) => {
        const id = safeText(row.detail_id, "asic");
        const target = safeText(row.target, "");
        const disabled = row.restart_disabled === "true";
        const disabledAttr = disabled ? " disabled" : "";
        const detailID = "asic-detail-" + id;
        const resultID = "asic-control-result-" + id;
        const details = [
          asicDetailItem("Why not mining / state", row.diagnostic),
          asicDetailItem("Active Pool", row.active_pool),
          asicDetailItem("Pool User", row.pool_user),
          asicDetailItem("Pool Legal", row.pool_legal),
          asicDetailItem("Protocol", row.protocol),
          asicDetailItem("Blocks 30m / 1h / 24h", safeText(row.blocks_30m, "n/a") + " / " + safeText(row.blocks_1h, "n/a") + " / " + safeText(row.blocks_24h, "n/a")),
          asicDetailItem("Rejects 30m", row.rejects_30m),
          asicDetailItem("Clock / Voltage", safeText(row.clock, "unknown") + " / " + safeText(row.voltage, "unknown")),
          asicDetailItem("Temp / Fan", safeText(row.temperature, "unknown") + " / " + safeText(row.fan, "unknown")),
          asicDetailItem("Telemetry", safeText(row.telemetry, "unknown") + " / " + safeText(row.telemetry_age, "unknown")),
          asicDetailItem("Telemetry Errors", row.telemetry_errors),
          asicDetailItem("Source", row.source),
        ].join("");
        return '<tr class="asic-summary-row" data-asic-row="' + escapeHTML(id) + '"><td style="font-weight: 600;">' + escapeHTML(row.mac) + '</td><td>' + escapeHTML(row.ip) + '</td><td>' + escapeHTML(row.status) + '</td><td>' + escapeHTML(row.ready) + '</td><td class="num">' + escapeHTML(row.hashrate) + '</td><td class="num">' + escapeHTML(row.shares) + '</td><td>' + escapeHTML(row.job_age) + '</td><td>' + escapeHTML(row.last) + '</td><td><span class="asic-row-action"><button type="button" class="ghost-button compact" data-asic-detail-toggle data-asic-detail-target="' + escapeHTML(detailID) + '">Restart</button></span></td></tr>' +
          '<tr id="' + escapeHTML(detailID) + '" class="asic-detail-row" data-detail-key="' + escapeHTML(id) + '" hidden><td colspan="9"><div class="asic-detail-panel"><div class="asic-detail-grid">' + details + '</div><form class="form-grid" hx-post="/actions/miner-control/restart" hx-target="#' + escapeHTML(resultID) + '" hx-swap="innerHTML" hx-confirm="Restart ASIC ' + escapeHTML(row.mac) + ' at ' + escapeHTML(row.ip) + ' now?"><input type="hidden" name="target" value="' + escapeHTML(target) + '"><div class="field"><label>ASIC Admin Password</label><input name="admin_password" type="password" autocomplete="current-password"' + disabledAttr + '></div><div class="field"><label>Control Token</label><input name="control_token" type="password" autocomplete="off"' + disabledAttr + '></div><div class="field"><label>Restart</label><button class="ghost-button" type="submit"' + disabledAttr + '>Restart ASIC</button></div></form><div class="muted">' + escapeHTML(row.restart_reason) + '</div><section id="' + escapeHTML(resultID) + '"></section></div></td></tr>';
      }).join("");
    }
    function asicDeviceRowsHTML(rows) {
      const data = Array.isArray(rows) ? rows : [];
      if (!data.length) return '<tr><td class="muted" colspan="18" style="padding: 18px 12px;">No ASIC device telemetry yet.</td></tr>';
      return data.map((row) => {
        return '<tr><td style="font-weight: 600;">' + escapeHTML(row.mac) + '</td><td>' + escapeHTML(row.ip) + '</td><td>' + escapeHTML(row.model) + '</td><td>' + escapeHTML(row.firmware) + '</td><td>' + escapeHTML(row.clock) + '</td><td>' + escapeHTML(row.voltage) + '</td><td>' + escapeHTML(row.chips) + '</td><td>' + escapeHTML(row.temp) + '</td><td>' + escapeHTML(row.fan) + '</td><td>' + escapeHTML(row.hashrate) + '</td><td>' + escapeHTML(row.powerplan) + '</td><td>' + escapeHTML(row.active_pool) + '</td><td>' + escapeHTML(row.pool_user) + '</td><td>' + escapeHTML(row.pool_legal) + '</td><td>' + escapeHTML(row.miner_status) + '</td><td class="num">' + escapeHTML(row.accepted) + '</td><td class="num">' + escapeHTML(row.hw_errors) + '</td><td>' + escapeHTML(row.telemetry) + ' / ' + escapeHTML(row.telemetry_age) + '</td></tr>';
      }).join("");
    }
    function stratumClientRowsHTML(rows) {
      const data = Array.isArray(rows) ? rows : [];
      if (!data.length) return '<tr><td class="muted" colspan="14" style="padding: 18px 12px;">No Stratum clients in job-state snapshot.</td></tr>';
      return data.map((row) => {
        return '<tr><td style="font-weight: 600;">' + escapeHTML(row.mac) + '</td><td>' + escapeHTML(row.ip) + '</td><td>' + escapeHTML(row.ready) + '</td><td>' + escapeHTML(row.reason) + '</td><td>' + escapeHTML(row.job_id) + '</td><td>' + escapeHTML(row.job_age) + '</td><td>' + escapeHTML(row.seq) + '</td><td>' + escapeHTML(row.backend) + '</td><td>' + escapeHTML(row.parent) + '</td><td class="num">' + escapeHTML(row.pdiff) + '</td><td class="num">' + escapeHTML(row.expired) + '</td><td class="num">' + escapeHTML(row.low_diff) + '</td><td>' + escapeHTML(row.subscribed) + '</td><td>' + escapeHTML(row.protocol) + '</td></tr>';
      }).join("");
    }
    function chainDAGRowsHTML(rows) {
      const data = Array.isArray(rows) ? rows : [];
      if (!data.length) return '<tr><td class="muted" colspan="8" style="padding: 18px 12px;">No chain topology rows yet.</td></tr>';
      return data.map((row) => {
        return '<tr><td class="num">' + escapeHTML(row.height) + '</td><td style="font-weight: 600;">' + escapeHTML(row.hash) + '</td><td>' + escapeHTML(row.parent) + '</td><td>' + escapeHTML(row.producer) + '</td><td>' + escapeHTML(row.local) + '</td><td class="num">' + escapeHTML(row.parents) + '</td><td class="num">' + escapeHTML(row.missing) + '</td><td>' + escapeHTML(row.time) + '</td></tr>';
      }).join("");
    }
    function peerTableHTML(payload) {
      const columns = Array.isArray(payload?.columns) ? payload.columns : [];
      const rows = Array.isArray(payload?.rows) ? payload.rows : [];
      if (!columns.length) {
        return '<div class="muted" style="padding: 18px 12px;">No peer columns returned by peer RPC.</div>';
      }
      const peerColumnTooltips = {
        fork_status: "Native DAG Main-chain classification. same_main_chain means the peer tip is proven on this node's Main chain; divergent_main_chain means the peer tip is known locally but not on this node's Main chain; common_main_chain_prefix means only a shared prefix is proven; unknown means evidence is incomplete.",
        same_native_fork: "Nullable native fork verdict. true means same_main_chain is proven; false means divergent_main_chain is proven; blank means the node has not proven either case.",
        fork_confidence: "Evidence level behind fork_status. main_tip is strongest; sync_point means only a shared Main-chain prefix; order mismatch or unknown sync point values keep the verdict conservative.",
        fork_reason: "Plain-language explanation for the exact fork classification branch, such as matching Main tip, known non-Main tip, shared prefix, or missing local evidence.",
      };
      const peerHashColumn = (column) => {
        const key = String(column || "").toLowerCase();
        return key.includes("hash") || key.includes("root") || key === "genesis";
      };
      const head = columns.map((column) => {
        const tooltip = peerColumnTooltips[column] || "";
        const classes = [];
        if (peerHashColumn(column)) classes.push("hash-cell");
        if (tooltip) classes.push("has-tooltip");
        const classAttr = classes.length ? ' class="' + classes.join(" ") + '"' : "";
        const tooltipAttr = tooltip ? ' data-tooltip="' + escapeHTML(tooltip) + '" tabindex="0"' : "";
        return '<th' + classAttr + tooltipAttr + '>' + escapeHTML(column) + '</th>';
      }).join("");
      const body = rows.length ? rows.map((row) => {
        return '<tr>' + columns.map((column) => {
          const value = row?.[column] ?? "";
          if (!peerHashColumn(column)) return '<td>' + escapeHTML(value) + '</td>';
          return '<td class="hash-cell" title="' + escapeHTML(value) + '"><span class="hash-cell-text">' + escapeHTML(value) + '</span></td>';
        }).join("") + '</tr>';
      }).join("") : '<tr><td class="muted" colspan="' + columns.length + '" style="padding: 18px 12px;">No peers returned by peer RPC.</td></tr>';
      return '<table class="data-table"><thead><tr>' + head + '</tr></thead><tbody id="peer-list-rows">' + body + '</tbody></table>';
    }
    function parseASICPerformanceSeries(raw) {
      try {
        const parsed = JSON.parse(raw || "[]");
        return Array.isArray(parsed) ? parsed : [];
      } catch (error) {
        return [];
      }
    }
    function writeASICPerformanceSeries(series) {
      window.asicPerformanceSeries = Array.isArray(series) ? series : [];
      const el = document.getElementById("asicPerformanceData");
      if (el) el.textContent = JSON.stringify(window.asicPerformanceSeries);
      drawASICPerformance();
    }
    function currentASICPerformanceSeries() {
      if (!Array.isArray(window.asicPerformanceSeries)) {
        const el = document.getElementById("asicPerformanceData");
        window.asicPerformanceSeries = parseASICPerformanceSeries(el ? el.textContent : "[]");
      }
      return window.asicPerformanceSeries;
    }
    function drawSmoothPath(ctx, points) {
      if (!points.length) return;
      ctx.moveTo(points[0].x, points[0].y);
      if (points.length === 1) return;
      if (points.length === 2) {
        ctx.lineTo(points[1].x, points[1].y);
        return;
      }
      for (let i = 0; i < points.length - 1; i++) {
        const p0 = points[Math.max(0, i - 1)];
        const p1 = points[i];
        const p2 = points[i + 1];
        const p3 = points[Math.min(points.length - 1, i + 2)];
        const tension = 0.05;
        const cp1x = p1.x + (p2.x - p0.x) * tension;
        const cp1y = p1.y + (p2.y - p0.y) * tension;
        const cp2x = p2.x - (p3.x - p1.x) * tension;
        const cp2y = p2.y - (p3.y - p1.y) * tension;
        ctx.bezierCurveTo(cp1x, cp1y, cp2x, cp2y, p2.x, p2.y);
      }
    }
    function niceAxisMax(value) {
      const raw = Math.max(Number(value) || 1, 1);
      const exponent = Math.floor(Math.log10(raw));
      const base = Math.pow(10, exponent);
      const normalized = raw / base;
      const nice = normalized <= 1 ? 1 : normalized <= 2 ? 2 : normalized <= 5 ? 5 : 10;
      return nice * base;
    }
    function producerTrendXTicks(minT, maxT, rangeHours) {
      if (!Number.isFinite(minT) || !Number.isFinite(maxT) || maxT <= minT) return [];
      const spanHours = (maxT - minT) / (60 * 60 * 1000);
      if (spanHours >= 720 || rangeHours >= 720) return evenlySpacedTimeTicks(minT, maxT, 4, "date");
      if (spanHours >= 168 || rangeHours >= 168) return dayTimeTicks(minT, maxT);
      if (spanHours >= 24 || rangeHours === 24) return intervalTimeTicks(minT, maxT, 2 * 60 * 60 * 1000, "time");
      return intervalTimeTicks(minT, maxT, 15 * 60 * 1000, "time");
    }
    function intervalTimeTicks(minT, maxT, stepMs, format) {
      const out = [];
      const start = Math.ceil(minT / stepMs) * stepMs;
      for (let t = start; t <= maxT + 1; t += stepMs) {
        out.push({t, label: formatTimeTick(t, format)});
      }
      if (out.length <= 2) {
        return evenlySpacedTimeTicks(minT, maxT, 4, format);
      }
      return out;
    }
    function dayTimeTicks(minT, maxT) {
      const out = [];
      const dayMs = 24 * 60 * 60 * 1000;
      const startDate = new Date(minT);
      const start = new Date(startDate.getFullYear(), startDate.getMonth(), startDate.getDate() + 1).getTime();
      for (let t = start; t <= maxT + 1; t += dayMs) {
        out.push({t, label: formatTimeTick(t, "day")});
      }
      if (!out.length) out.push(...evenlySpacedTimeTicks(minT, maxT, 4, "day"));
      return out;
    }
    function evenlySpacedTimeTicks(minT, maxT, divisions, format) {
      const out = [];
      const count = Math.max(1, divisions);
      for (let i = 0; i <= count; i++) {
        const t = minT + ((maxT - minT) * i / count);
        out.push({t, label: formatTimeTick(t, format)});
      }
      return out;
    }
    function formatTimeTick(t, format) {
      const date = new Date(t);
      if (format === "day") {
        return date.toLocaleDateString(undefined, {weekday: "short"});
      }
      if (format === "date") {
        return date.toLocaleDateString(undefined, {month: "short", day: "numeric"});
      }
      return date.toLocaleTimeString(undefined, {hour: "2-digit", minute: "2-digit"});
    }
    function drawASICPerformance() {
      const canvas = document.getElementById("asicPerformanceChart");
      const legend = document.getElementById("asicPerformanceLegend");
      if (!canvas) return;
      const rect = canvas.parentElement.getBoundingClientRect();
      const ratio = window.devicePixelRatio || 1;
      const width = Math.max(360, Math.floor(rect.width));
      const height = Math.max(220, Math.floor(rect.height));
      canvas.width = width * ratio; canvas.height = height * ratio;
      const ctx = canvas.getContext("2d"); ctx.setTransform(ratio, 0, 0, ratio, 0, 0);
      ctx.clearRect(0, 0, width, height); ctx.fillStyle = "#171421"; ctx.fillRect(0, 0, width, height);
      const rawSeries = currentASICPerformanceSeries().map((s) => ({
        ...s,
        points: Array.isArray(s.points) ? s.points.map((p) => ({
          ...p,
          t: Date.parse(p.t),
          accepted: Number(p.accepted) || 0,
          hashrate: Number(p.hashrate) || 0,
          avg_hashrate: Number(p.avg_hashrate) || 0,
          temperature: Number(p.temperature) || 0,
        })).filter((p) => Number.isFinite(p.t)) : [],
      })).filter((s) => s.points.length);
      const rawPoints = rawSeries.flatMap((s) => s.points);
      const latestT = rawPoints.length ? Math.max(...rawPoints.map((p) => p.t)) : 0;
      const sixHourMS = 6 * 60 * 60 * 1000;
      const windowStart = latestT - sixHourMS;
      const series = rawSeries.map((s) => ({
        ...s,
        points: s.points.filter((p) => p.t >= windowStart),
      })).filter((s) => s.points.length);
      const allPoints = series.flatMap((s) => s.points);
      if (!allPoints.length) {
        if (legend) legend.innerHTML = "";
        ctx.fillStyle = "#b8b0c7"; ctx.font = "12px Inter, sans-serif";
        ctx.fillText("ASIC hashrate history will appear after miner telemetry is observed.", 16, 28);
        return;
      }
      const minT = Math.min(...allPoints.map((p) => p.t)), maxT = Math.max(...allPoints.map((p) => p.t));
      const useHashrate = allPoints.some((p) => p.hashrate > 0 || p.avg_hashrate > 0);
      const valueForPoint = (p) => useHashrate ? (p.hashrate || p.avg_hashrate || 0) : p.accepted;
      const maxV = Math.max(...allPoints.map(valueForPoint), 1);
      const scaleMax = niceAxisMax(maxV);
      const m = {l: 54, r: 22, t: 28, b: 42}, w = width - m.l - m.r, h = height - m.t - m.b;
      ctx.strokeStyle = "#2d2938"; ctx.lineWidth = 1;
      ctx.fillStyle = "#b8b0c7"; ctx.font = "12px Inter, sans-serif";
      ctx.fillText(useHashrate ? "Miner hashrate (GH/s)" : "Blocks / 10m", m.l, 18);
      ctx.textAlign = "right"; ctx.textBaseline = "middle";
      for (let i = 0; i <= 4; i++) {
        const y = m.t + h * i / 4;
        const tickValue = scaleMax - (scaleMax * i / 4);
        ctx.strokeStyle = "#2d2938"; ctx.beginPath(); ctx.moveTo(m.l, y); ctx.lineTo(width - m.r, y); ctx.stroke();
        ctx.fillStyle = "#b8b0c7"; ctx.fillText(Math.round(tickValue).toString(), m.l - 8, y);
      }
      ctx.textAlign = "center"; ctx.textBaseline = "top"; ctx.font = "11px Inter, sans-serif";
      const ticks = producerTrendXTicks(minT, maxT, 24);
      ticks.forEach((tick) => {
        const x = m.l + ((tick.t - minT) / Math.max(1, maxT - minT)) * w;
        ctx.strokeStyle = "#2d2938"; ctx.beginPath(); ctx.moveTo(x, m.t); ctx.lineTo(x, m.t + h + 6); ctx.stroke();
        ctx.fillStyle = "#b8b0c7"; ctx.fillText(tick.label, x, m.t + h + 12);
      });
      if (legend) {
        legend.innerHTML = "";
        series.forEach((s) => {
          const key = document.createElement("span");
          key.className = "legend-key";
          key.style.setProperty("--key-color", s.color || "#2bd4a3");
          const latest = s.points.reduce((best, p) => !best || p.t > best.t ? p : best, null);
          const latestHashrate = latest ? (latest.hashrate || latest.avg_hashrate || 0) : 0;
          key.textContent = useHashrate && latestHashrate > 0 ? (s.label || "unknown") + " " + latestHashrate.toFixed(1) + " GH/s" : (s.label || "unknown");
          legend.appendChild(key);
        });
      }
      series.forEach((s) => {
        const plotted = s.points.map((p) => ({
          x: m.l + ((p.t - minT) / Math.max(1, maxT - minT)) * w,
          y: m.t + h - (valueForPoint(p) / scaleMax) * h,
          value: valueForPoint(p),
        }));
        ctx.strokeStyle = s.color || "#2bd4a3";
        ctx.lineWidth = s.ready ? 3 : 2;
        ctx.globalAlpha = s.ready ? 1 : 0.62;
        ctx.lineCap = "round"; ctx.lineJoin = "round"; ctx.beginPath();
        drawSmoothPath(ctx, plotted);
        ctx.stroke();
        const activePlotted = plotted.filter((p) => p.value > 0);
        if (activePlotted.length === 1) {
          const point = activePlotted[0];
          const span = 10;
          ctx.beginPath();
          ctx.moveTo(Math.max(m.l, point.x - span), point.y);
          ctx.lineTo(Math.min(width - m.r, point.x + span), point.y);
          ctx.stroke();
        }
        ctx.globalAlpha = 1;
      });
    }
    function parseChainDAGGraph(raw) {
      try {
        const parsed = JSON.parse(raw || "{}");
        return parsed && typeof parsed === "object" ? parsed : {nodes: [], edges: []};
      } catch (error) {
        return {nodes: [], edges: []};
      }
    }
    function chainDAGNodeSignature(graph) {
      return (Array.isArray(graph && graph.nodes) ? graph.nodes : [])
        .map((node) => chainDAGNodeKey(node))
        .filter(Boolean)
        .sort()
        .join("|");
    }
    function chainDAGUsefulNodeCount(graph) {
      return (Array.isArray(graph && graph.nodes) ? graph.nodes : []).filter((node) => chainDAGNodeKey(node)).length;
    }
    function chainDAGVisibleLimit(graph, patch) {
      const currentWindow = graph && graph.window && Number(graph.window.requested);
      const patchWindow = patch && Number(patch.window_limit);
      const limit = Number.isFinite(currentWindow) && currentWindow > 0 ? currentWindow : patchWindow;
      return Math.max(1, Math.min(90, Number.isFinite(limit) && limit > 0 ? limit : 90));
    }
    function chainDAGMotionResetNeeded(previous, next, incremental) {
      if (incremental || !previous || !next) return false;
      const previousNodes = new Set((Array.isArray(previous.nodes) ? previous.nodes : []).map(chainDAGNodeKey).filter(Boolean));
      const nextNodes = new Set((Array.isArray(next.nodes) ? next.nodes : []).map(chainDAGNodeKey).filter(Boolean));
      if (!previousNodes.size || !nextNodes.size) return false;
      if (String(previous.mode || "") !== String(next.mode || "") || String(previous.source || "") !== String(next.source || "")) return true;
      let retained = 0;
      nextNodes.forEach((key) => { if (previousNodes.has(key)) retained++; });
      const retention = retained / Math.max(previousNodes.size, nextNodes.size);
      return retention < 0.55 || Math.abs(previousNodes.size - nextNodes.size) > 48;
    }
    function resetChainDAGMotion() {
      const motion = window.chainDAGMotion;
      if (motion && motion.raf) window.cancelAnimationFrame(motion.raf);
      window.chainDAGMotion = null;
      if (window.chainDAGPulseRAF) {
        window.cancelAnimationFrame(window.chainDAGPulseRAF);
        window.chainDAGPulseRAF = 0;
      }
    }
    function writeChainDAGGraph(graph, options = {}) {
      const nextGraph = graph && typeof graph === "object" ? graph : {nodes: [], edges: []};
      const previousGraph = window.chainDAGGraph;
      if (!options.allowSparse && chainDAGUsefulNodeCount(nextGraph) < 2 && chainDAGUsefulNodeCount(previousGraph) >= 2) return;
      if (chainDAGMotionResetNeeded(previousGraph, nextGraph, Boolean(options.incremental))) resetChainDAGMotion();
      window.chainDAGGraph = nextGraph;
      window.chainDAGGraphSignature = chainDAGNodeSignature(window.chainDAGGraph);
      const el = document.getElementById("chainDAGData");
      if (el) el.textContent = JSON.stringify(window.chainDAGGraph);
      drawChainDAG();
    }
    function chainDAGNodeKey(node) { return String(node && (node.hash || node.id) || "").toLowerCase(); }
    function chainDAGNodeOrder(node, fallback = 0) {
      const value = Number(node && (node.layout_order || node.order || node.height));
      return Number.isFinite(value) ? value : fallback;
    }
    function chainDAGEdgeKey(edge) {
      return [edge && edge.type || "dag_parent", String(edge && edge.child || "").toLowerCase(), String(edge && edge.parent || "").toLowerCase()].join("|");
    }
    function parseChainDAGPatch(raw) {
      try {
        if (!raw) return null;
        if (typeof raw === "object") return raw;
        const parsed = JSON.parse(String(raw));
        return parsed && typeof parsed === "object" ? parsed : null;
      } catch (error) {
        return null;
      }
    }
    function applyChainDAGPatch(raw) {
      if (activeLiveTab() !== "chain-dag") return false;
      const patch = parseChainDAGPatch(raw);
      if (!patch || !Array.isArray(patch.nodes) || !patch.nodes.length) return false;
      const graph = currentChainDAGGraph();
      const nodeLimit = chainDAGVisibleLimit(graph, patch);
      const nodesByKey = new Map();
      (Array.isArray(graph.nodes) ? graph.nodes : []).forEach((node) => {
        const key = chainDAGNodeKey(node);
        if (key) nodesByKey.set(key, node);
      });
      patch.nodes.forEach((node) => {
        const key = chainDAGNodeKey(node);
        if (key) nodesByKey.set(key, {...(nodesByKey.get(key) || {}), ...node});
      });
      const nodes = Array.from(nodesByKey.values()).sort((a, b) => chainDAGNodeOrder(b) - chainDAGNodeOrder(a)).slice(0, nodeLimit);
      const keptNodes = new Set(nodes.map(chainDAGNodeKey).filter(Boolean));
      const edgesByKey = new Map();
      (Array.isArray(graph.edges) ? graph.edges : []).forEach((edge) => {
        const child = String(edge && edge.child || "").toLowerCase();
        const parent = String(edge && edge.parent || "").toLowerCase();
        if (!keptNodes.has(child)) return;
        if (parent && !keptNodes.has(parent) && edge.known !== false) return;
        edgesByKey.set(chainDAGEdgeKey(edge), edge);
      });
      (Array.isArray(patch.edges) ? patch.edges : []).forEach((edge) => {
        const child = String(edge && edge.child || "").toLowerCase();
        if (!keptNodes.has(child)) return;
        edgesByKey.set(chainDAGEdgeKey(edge), edge);
      });
      writeChainDAGGraph({
        ...graph,
        mode: patch.mode || graph.mode,
        source: patch.source || graph.source,
        best_effort: patch.best_effort ?? graph.best_effort,
        coverage: patch.coverage || graph.coverage,
        window: {...(graph.window || {}), requested: nodeLimit, available: nodes.length, truncated: nodes.length >= nodeLimit},
        nodes,
        edges: Array.from(edgesByKey.values()),
      }, {incremental: true});
      return true;
    }
    function currentChainDAGGraph() {
      if (!window.chainDAGGraph) {
        const el = document.getElementById("chainDAGData");
        window.chainDAGGraph = parseChainDAGGraph(el ? el.textContent : "{}");
      }
      return window.chainDAGGraph;
    }
    function drawChainDAG() {
      const canvas = document.getElementById("chainDAGCanvas");
      if (!canvas) return;
      const graph = currentChainDAGGraph();
      const visibleLimit = chainDAGVisibleLimit(graph, null);
      const nodes = (Array.isArray(graph.nodes) ? graph.nodes : []).slice(0, visibleLimit);
      const edges = Array.isArray(graph.edges) ? graph.edges : [];
      const wrap = canvas.parentElement;
      const rect = wrap.getBoundingClientRect();
      const ratio = Math.min(window.devicePixelRatio || 1, 2);
      const width = Math.max(520, Math.floor(rect.width), Math.min(2800, nodes.length * 20 + 180));
      const height = Math.max(350, Math.floor(rect.height || 0));
      canvas.style.width = width + "px";
      canvas.style.height = height + "px";
      const maxScrollLeft = Math.max(0, width - Math.floor(rect.width || wrap.clientWidth || width));
      const previousMaxScrollLeft = Number(wrap.dataset.chainDagMaxScrollLeft || maxScrollLeft);
      const wasAtLiveEdge = !wrap.dataset.chainDagScrollReady || Math.abs(wrap.scrollLeft - previousMaxScrollLeft) < 48;
      wrap.dataset.chainDagScrollReady = "1";
      wrap.dataset.chainDagMaxScrollLeft = String(maxScrollLeft);
      if (wasAtLiveEdge) wrap.scrollLeft = maxScrollLeft;
      canvas.width = width * ratio; canvas.height = height * ratio;
      const ctx = canvas.getContext("2d");
      function resetCanvasState() {
        ctx.globalAlpha = 1;
        ctx.globalCompositeOperation = "source-over";
        ctx.shadowColor = "transparent";
        ctx.shadowBlur = 0;
        ctx.lineCap = "butt";
        ctx.lineJoin = "miter";
        ctx.setLineDash([]);
      }
      ctx.setTransform(1, 0, 0, 1, 0, 0);
      resetCanvasState();
      ctx.clearRect(0, 0, canvas.width, canvas.height);
      ctx.setTransform(ratio, 0, 0, ratio, 0, 0);
      resetCanvasState();
      ctx.fillStyle = "#171421"; ctx.fillRect(0, 0, width, height);
      if (nodes.length < 2) {
        ctx.fillStyle = "#b8b0c7"; ctx.font = "12px Inter, sans-serif";
        ctx.fillText("Recent chain topology will appear after Redis has enough live DAG data.", 16, 28);
        return;
      }
      function hashKey(value) { return String(value || "").toLowerCase(); }
      function hashBucket(value) {
        let h = 0;
        const text = hashKey(value);
        for (let i = 0; i < text.length; i++) h = ((h << 5) - h + text.charCodeAt(i)) | 0;
        return Math.abs(h);
      }
      function nodeKey(node) { return hashKey(node && (node.hash || node.id)); }
      function nodeOrder(node, fallback) {
        const value = Number(node && (node.layout_order || node.order || node.height));
        return Number.isFinite(value) ? value : fallback;
      }
      function clamp(value, min, max) { return Math.max(min, Math.min(max, value)); }
      function nodeProducer(node) { return hashKey(node && node.producer); }
      function colorWithAlpha(color, alpha) {
        const hex = safeColor(color).slice(1);
        const red = parseInt(hex.slice(0, 2), 16);
        const green = parseInt(hex.slice(2, 4), 16);
        const blue = parseInt(hex.slice(4, 6), 16);
        return "rgba(" + red + ", " + green + ", " + blue + ", " + alpha + ")";
      }
      function producerColor(node, fallback = "#6b7280") {
        if (!node || node.producer_pending || !nodeProducer(node)) return fallback;
        return safeColor(node.producer_color);
      }
      function semanticFill(node, tone, multi, trunkNode) {
        const state = String(node && node.dag_set || "").toLowerCase();
        if (state === "tip-valid") return "#17345c";
        if (state === "tip-invalid") return "#4c1d2b";
        if (node.local_pool) return "#103d36";
        if (tone === "blue") return multi ? "#1f3d66" : "#172d49";
        if (tone === "red") return multi ? "#542233" : "#3b1d28";
        return "#202936";
      }
      const nodeByKey = new Map();
      nodes.forEach((node) => {
        const key = nodeKey(node);
        if (key) nodeByKey.set(key, node);
      });
      const parentEdges = edges.filter((edge) => edge.type !== "linear_path");
      const parentEdgeByPair = new Map();
      const childrenByKey = new Map();
      parentEdges.forEach((edge) => {
        const child = hashKey(edge.child);
        const parent = hashKey(edge.parent);
        if (!child || !parent) return;
        parentEdgeByPair.set(child + "|" + parent, edge);
        if (!childrenByKey.has(parent)) childrenByKey.set(parent, []);
        childrenByKey.get(parent).push(child);
      });
      function parentsOf(node) {
        return Array.isArray(node && node.parents) ? node.parents.map(hashKey).filter((key) => key && nodeByKey.has(key)) : [];
      }
      function graphScore(node) {
        const key = nodeKey(node);
        const knownParents = Number(node && node.known_parent_count || 0);
        const parentCount = Number(node && node.parent_count || 0);
        const children = childrenByKey.get(key) || [];
        return children.length * 4 + knownParents * 3 + parentCount + (node && node.multi_parent ? 2 : 0);
      }
      function deriveChainDAGTrunk(sortedNewestFirst) {
        const path = [];
        const visited = new Set();
        let current = sortedNewestFirst[0];
        while (current) {
          const currentKey = nodeKey(current);
          if (!currentKey || visited.has(currentKey)) break;
          visited.add(currentKey);
          path.push(currentKey);
          const mainParent = hashKey(current.main_parent_hash);
          const currentOrder = nodeOrder(current, 0);
          const candidates = parentsOf(current).filter((key) => !visited.has(key));
          if (!candidates.length) break;
          candidates.sort((a, b) => {
            const nodeA = nodeByKey.get(a);
            const nodeB = nodeByKey.get(b);
            const mainA = a === mainParent ? 1000 : 0;
            const mainB = b === mainParent ? 1000 : 0;
            const distanceA = Math.abs(currentOrder - nodeOrder(nodeA, currentOrder));
            const distanceB = Math.abs(currentOrder - nodeOrder(nodeB, currentOrder));
            return (mainB + graphScore(nodeB) * 10 - distanceB) - (mainA + graphScore(nodeA) * 10 - distanceA);
          });
          current = nodeByKey.get(candidates[0]);
        }
        if (path.length < Math.min(8, sortedNewestFirst.length)) {
          sortedNewestFirst.slice(0, Math.min(12, sortedNewestFirst.length)).forEach((node) => {
            const key = nodeKey(node);
            if (key && !visited.has(key) && graphScore(node) >= 2) {
              visited.add(key);
              path.push(key);
            }
          });
        }
        return {path, set: visited};
      }
      const sortedNewestFirst = nodes.slice().sort((a, b) => nodeOrder(b, 0) - nodeOrder(a, 0));
      const trunk = deriveChainDAGTrunk(sortedNewestFirst);
      const trunkSet = trunk.set;
      const adjacent = new Map();
      function addAdjacent(a, b) {
        if (!a || !b || !nodeByKey.has(a) || !nodeByKey.has(b)) return;
        if (!adjacent.has(a)) adjacent.set(a, []);
        if (!adjacent.has(b)) adjacent.set(b, []);
        adjacent.get(a).push(b);
        adjacent.get(b).push(a);
      }
      parentEdges.forEach((edge) => addAdjacent(hashKey(edge.child), hashKey(edge.parent)));
      const branchDepth = new Map();
      const queue = [];
      trunk.path.forEach((key) => {
        branchDepth.set(key, 0);
        queue.push(key);
      });
      for (let i = 0; i < queue.length; i++) {
        const key = queue[i];
        const depth = branchDepth.get(key) || 0;
        if (depth >= 4) continue;
        (adjacent.get(key) || []).forEach((next) => {
          if (!branchDepth.has(next)) {
            branchDepth.set(next, depth + 1);
            queue.push(next);
          }
        });
      }
      const plotLeft = 72, plotRight = 52, top = 58, bottom = 62;
      const centerY = top + (height - top - bottom) / 2;
      const orders = nodes.map((node, index) => nodeOrder(node, index)).filter((v) => Number.isFinite(v));
      const minOrder = Math.min(...orders);
      const maxOrder = Math.max(...orders);
      const orderSpan = Math.max(1, maxOrder - minOrder);
      const uniqueOrders = Array.from(new Set(orders)).sort((a, b) => a - b);
      const orderRankByValue = new Map(uniqueOrders.map((order, index) => [String(order), index]));
      const orderRankSpan = Math.max(1, uniqueOrders.length - 1);
      const laneGap = clamp((height - top - bottom) / 13, 34, 48);
      const laneLimit = Math.max(4, Math.floor((height - top - bottom) / (2 * laneGap)));
      const producerLaneIndexes = window.chainDAGProducerLaneIndexes instanceof Map ? window.chainDAGProducerLaneIndexes : new Map();
      Array.from(new Set(nodes.map(nodeProducer).filter(Boolean))).sort().forEach((producer) => {
        if (!producerLaneIndexes.has(producer)) producerLaneIndexes.set(producer, producerLaneIndexes.size);
      });
      window.chainDAGProducerLaneIndexes = producerLaneIndexes;
      const producerLaneByKey = new Map();
      producerLaneIndexes.forEach((index, producer) => {
        const rank = Math.floor(index / 2) + 1;
        const sign = index % 2 === 0 ? -1 : 1;
        producerLaneByKey.set(producer, clamp(sign * rank, -laneLimit, laneLimit));
      });
      const laneByKey = new Map();
      const positions = new Map();
      const occupied = new Map();
      function xForOrder(order) {
        const rank = orderRankByValue.get(String(order));
        const progress = Number.isFinite(rank) ? rank / orderRankSpan : (order - minOrder) / orderSpan;
        return plotLeft + progress * (width - plotLeft - plotRight);
      }
      function animateChainDAGPositions(targetPositions) {
        const now = window.performance && window.performance.now ? window.performance.now() : Date.now();
        const motion = window.chainDAGMotion || {current: null, targetSignature: ""};
        const signature = Array.from(targetPositions.entries()).map(([key, pos]) => key + ":" + Math.round(pos.x) + ":" + Math.round(pos.y)).join("|");
        if (motion.targetSignature !== signature || motion.width !== width || motion.height !== height) {
          const previous = motion.current instanceof Map ? motion.current : null;
          const from = new Map();
          targetPositions.forEach((target, key) => {
            const prior = previous ? previous.get(key) : null;
            if (prior) {
              from.set(key, {...target, x: prior.x, y: prior.y, lane: prior.lane});
            } else {
              from.set(key, {...target, x: previous ? width + 32 : target.x, y: target.y});
            }
          });
          motion.from = from;
          motion.targets = new Map(targetPositions);
          motion.startedAt = now;
          motion.durationMs = previous ? 720 : 0;
          motion.targetSignature = signature;
          motion.width = width;
          motion.height = height;
          if (motion.raf) {
            window.cancelAnimationFrame(motion.raf);
            motion.raf = 0;
          }
        }
        const duration = Math.max(0, Number(motion.durationMs || 0));
        const rawProgress = duration === 0 ? 1 : clamp((now - Number(motion.startedAt || now)) / duration, 0, 1);
        const eased = 1 - Math.pow(1 - rawProgress, 3);
        const rendered = new Map();
        const targets = motion.targets instanceof Map ? motion.targets : targetPositions;
        targets.forEach((target, key) => {
          const start = motion.from instanceof Map ? motion.from.get(key) : null;
          if (!start) {
            rendered.set(key, target);
            return;
          }
          rendered.set(key, {
            ...target,
            x: start.x + (target.x - start.x) * eased,
            y: start.y + (target.y - start.y) * eased,
            lane: rawProgress < 1 ? start.lane : target.lane,
          });
        });
        motion.current = rendered;
        window.chainDAGMotion = motion;
        if (rawProgress < 1 && !motion.raf) {
          motion.raf = window.requestAnimationFrame(() => {
            motion.raf = 0;
            drawChainDAG();
          });
        }
        return rendered;
      }
      function laneFree(column, lane) {
        for (let col = column - 1; col <= column + 1; col++) {
          if ((occupied.get(col) || new Set()).has(lane)) return false;
        }
        return true;
      }
      function reserveLane(column, desired, sign) {
        const candidates = [desired];
        for (let step = 1; step <= laneLimit; step++) {
          candidates.push(desired + step * sign, desired - step * sign, step * sign, -step * sign);
        }
        for (const candidate of candidates) {
          const lane = clamp(Math.round(candidate), -laneLimit, laneLimit);
          if (lane === 0) continue;
          if (laneFree(column, lane)) {
            if (!occupied.has(column)) occupied.set(column, new Set());
            occupied.get(column).add(lane);
            return lane;
          }
        }
        const fallback = clamp(desired, -laneLimit, laneLimit) || sign;
        if (!occupied.has(column)) occupied.set(column, new Set());
        occupied.get(column).add(fallback);
        return fallback;
      }
      function branchSign(key) {
        return (hashBucket(key) % 2 === 0) ? -1 : 1;
      }
      function branchLane(node, x) {
        const key = nodeKey(node);
        const producerLane = producerLaneByKey.get(nodeProducer(node));
        if (trunkSet.has(key)) return Number.isFinite(producerLane) ? producerLane : 0;
        if (Number.isFinite(producerLane)) return producerLane;
        const depth = clamp(branchDepth.get(key) || 3, 1, 4);
        const sign = branchSign(key);
        const desired = sign * clamp(depth + (hashBucket(key) % 3), 1, laneLimit);
        return reserveLane(Math.round(x / 18), desired, sign);
      }
      sortedNewestFirst.forEach((node, index) => {
        const key = nodeKey(node);
        const order = nodeOrder(node, index);
        const x = xForOrder(order);
        const lane = branchLane(node, x);
        const jitter = lane === 0 ? 0 : ((hashBucket(key) % 5) - 2) * 1.4;
        const y = clamp(centerY + lane * laneGap + jitter, top, height - bottom);
        laneByKey.set(key, lane);
        positions.set(key, {x: clamp(x, plotLeft, width - plotRight), y, lane, node});
      });
      const animatedPositions = animateChainDAGPositions(positions);
      positions.clear();
      animatedPositions.forEach((position, key) => positions.set(key, position));
      function drawOrderTicks() {
        ctx.save();
        ctx.strokeStyle = "rgba(184, 176, 199, 0.14)";
        ctx.lineWidth = 1;
        const tickCount = Math.min(8, sortedNewestFirst.length);
        if (tickCount <= 1) {
          ctx.restore();
          return;
        }
        const used = new Set();
        for (let step = 0; step < tickCount; step++) {
          const index = Math.round(step * (sortedNewestFirst.length - 1) / Math.max(1, tickCount - 1));
          if (used.has(index)) continue;
          used.add(index);
          const node = sortedNewestFirst[index];
          const x = xForOrder(nodeOrder(node, index));
          ctx.beginPath();
          ctx.moveTo(x, height - bottom + 10);
          ctx.lineTo(x, height - bottom + 22);
          ctx.stroke();
        }
        ctx.restore();
      }
      function drawTrunkTrace() {
        for (let index = 0; index + 1 < trunk.path.length; index++) {
          const child = trunk.path[index];
          const parent = trunk.path[index + 1];
          const edge = parentEdgeByPair.get(child + "|" + parent);
          if (edge) drawEdge(edge, true);
        }
      }
      function nodeSize(node) {
        const key = nodeKey(node);
        const multi = Boolean(node && node.multi_parent) || Number(node && node.parent_count || 0) > 1;
        const trunkNode = trunkSet.has(key);
        return {
          w: trunkNode ? 16 : (multi ? 20 : 16),
          h: trunkNode ? 10 : (multi ? 12 : 10),
        };
      }
      function laneTone(lane) {
        if (lane === 0) return "trunk";
        return lane < 0 ? "blue" : "red";
      }
      function drawEdge(edge, glow, emphasis = false) {
        const child = positions.get(hashKey(edge.child));
        const parent = positions.get(hashKey(edge.parent));
        if (!child) return;
        const missingSign = child.lane < 0 ? -1 : 1;
        const target = parent || {x: Math.max(plotLeft, child.x - 58), y: clamp(child.y + missingSign * 28, top, height - bottom), node: null};
        const direction = child.x >= target.x ? 1 : -1;
        const parentSize = parent ? nodeSize(parent.node) : {w: 8, h: 8};
        const childSize = nodeSize(child.node);
        const start = {x: target.x + direction * parentSize.w / 2, y: target.y};
        const end = {x: child.x - direction * childSize.w / 2, y: child.y};
        const mid = Math.max(18, Math.abs(end.x - start.x) * 0.44);
        ctx.save();
        if (edge.known === false) {
          ctx.strokeStyle = emphasis ? "rgba(245, 196, 94, 0.78)" : "rgba(245, 196, 94, 0.58)";
          ctx.setLineDash([5, 5]);
        } else {
          const lane = parent ? Math.abs(parent.lane) >= Math.abs(child.lane) ? parent.lane : child.lane : child.lane;
          const tone = laneTone(lane);
          const parentColor = parent ? producerColor(parent.node, tone === "red" ? "#fb7185" : "#60a5fa") : "#f5c45e";
          const childColor = producerColor(child.node, tone === "red" ? "#fb7185" : "#60a5fa");
          const gradient = ctx.createLinearGradient(start.x, start.y, end.x, end.y);
          gradient.addColorStop(0, colorWithAlpha(parentColor, glow ? 0.62 : (emphasis ? 0.52 : 0.28)));
          gradient.addColorStop(1, colorWithAlpha(childColor, glow ? 0.92 : (emphasis ? 0.86 : 0.58)));
          ctx.strokeStyle = gradient;
          if (glow) {
            ctx.globalCompositeOperation = "lighter";
            ctx.shadowColor = colorWithAlpha(childColor, 0.48);
            ctx.shadowBlur = 9;
          }
        }
        ctx.lineWidth = glow ? 2.35 : (emphasis ? 1.75 : 1.15);
        ctx.beginPath();
        ctx.moveTo(start.x, start.y);
        ctx.bezierCurveTo(start.x + direction * mid, start.y, end.x - direction * mid, end.y, end.x, end.y);
        ctx.stroke();
        ctx.restore();
      }
      parentEdges.forEach((edge) => drawEdge(edge, false));
      drawOrderTicks();
      parentEdges.forEach((edge) => {
        const childNode = nodeByKey.get(hashKey(edge.child));
        if (edge.known === false || Number(childNode && childNode.parent_count || 0) > 1) {
          drawEdge(edge, false, true);
        }
      });
      drawTrunkTrace();
      function roundRect(x, y, w, h, r) {
        const rr = Math.min(r, w / 2, h / 2);
        ctx.beginPath();
        ctx.moveTo(x + rr, y);
        ctx.arcTo(x + w, y, x + w, y + h, rr);
        ctx.arcTo(x + w, y + h, x, y + h, rr);
        ctx.arcTo(x, y + h, x, y, rr);
        ctx.arcTo(x, y, x + w, y, rr);
        ctx.closePath();
      }
      function drawNode(node) {
        const key = nodeKey(node);
        const pos = positions.get(key);
        if (!pos) return;
        const lane = laneByKey.get(key) || 0;
        const tone = laneTone(lane);
        const multi = Boolean(node.multi_parent) || Number(node.parent_count || 0) > 1;
        const local = Boolean(node.local_pool);
        const trunkNode = trunkSet.has(key);
        const size = nodeSize(node);
        const w = size.w;
        const h = size.h;
        const x = pos.x - w / 2;
        const y = pos.y - h / 2;
        const stroke = producerColor(node, tone === "red" ? "#fb7185" : "#60a5fa");
        const pendingProducer = Boolean(node.producer_pending) || !nodeProducer(node);
        ctx.save();
        if (node.tip) {
          const pulse = 0.5 + 0.5 * Math.sin((window.performance && window.performance.now ? window.performance.now() : Date.now()) / 260);
          ctx.strokeStyle = colorWithAlpha(stroke, 0.28 + pulse * 0.24);
          ctx.lineWidth = 1.4;
          ctx.shadowColor = colorWithAlpha(stroke, 0.54);
          ctx.shadowBlur = 14 + pulse * 10;
          roundRect(x - 7, y - 6, w + 14, h + 12, 6);
          ctx.stroke();
        }
        ctx.fillStyle = semanticFill(node, tone, multi, trunkNode);
        ctx.strokeStyle = stroke;
        ctx.shadowColor = local ? colorWithAlpha(stroke, 0.74) : "transparent";
        ctx.shadowBlur = local ? 14 : 0;
        ctx.lineWidth = multi ? 1.35 : 1;
        if (pendingProducer) ctx.setLineDash([3, 3]);
        roundRect(x, y, w, h, 3);
        ctx.fill();
        ctx.stroke();
        ctx.setLineDash([]);
        if (multi && nodes.length <= 90) {
          ctx.fillStyle = "#dbeafe";
          ctx.font = "9px Inter, sans-serif";
          ctx.textAlign = "center";
          ctx.textBaseline = "middle";
          const label = String(Number(node.parent_count || 0));
          ctx.fillText(label, pos.x, pos.y + 0.5);
        }
        ctx.restore();
      }
      nodes.filter((node) => !trunkSet.has(nodeKey(node))).forEach(drawNode);
      nodes.filter((node) => trunkSet.has(nodeKey(node))).forEach(drawNode);
      ctx.save();
      ctx.fillStyle = "rgba(184, 176, 199, 0.88)";
      ctx.font = "12px Inter, sans-serif";
      ctx.textAlign = "left";
      ctx.textBaseline = "alphabetic";
      ctx.fillText("older", plotLeft, height - 24);
      ctx.textAlign = "right";
      ctx.fillText("newest", width - plotRight, height - 24);
      ctx.restore();
      ctx.fillStyle = "#b8b0c7"; ctx.font = "12px Inter, sans-serif"; ctx.textAlign = "left"; ctx.textBaseline = "alphabetic";
      ctx.fillText(String(graph.mode || "native DAG") + " / " + String(graph.source || "redis"), 16, 20);
      const legend = [
        ["#60a5fa", "upper"],
        ["#fb7185", "lower"],
        ["#f5c45e", "missing"],
        ["#b8b0c7", "trace glow"],
      ];
      let lx = 16;
      legend.forEach((item) => {
        ctx.fillStyle = item[0];
        roundRect(lx, 34, 18, 8, 3);
        ctx.fill();
        ctx.fillStyle = "#b8b0c7";
        ctx.font = "11px Inter, sans-serif";
        ctx.fillText(item[1], lx + 24, 42);
        lx += 76;
      });
      if (nodes.some((node) => node.tip) && activeLiveTab() === "chain-dag" && !window.chainDAGPulseRAF) {
        window.chainDAGPulseRAF = window.requestAnimationFrame(() => {
          window.chainDAGPulseRAF = 0;
          drawChainDAG();
        });
      }
    }
    function minerEarningsRowsHTML(rows) {
      const data = Array.isArray(rows) ? rows : [];
      if (!data.length) return '<div class="muted">No miner earnings rows in API payload.</div>';
      return data.map((row) => {
        return '<div class="row-card"><div style="overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-weight: 600;">' + escapeHTML(row.wallet) + '</div><div style="text-align: right; font-weight: 700;">' + escapeHTML(row.avg) + ' BDAG/h</div><div style="text-align: right;" class="muted">' + escapeHTML(row.total) + ' BDAG</div></div>';
      }).join("");
    }
    function globalPoolRowsHTML(rows) {
      const data = Array.isArray(rows) ? rows : [];
      if (!data.length) return '<tr><td class="muted" colspan="4" style="padding: 18px 12px;">No global producer rows in API payload.</td></tr>';
      return data.map((row) => {
        const rowClass = row.local === "true" ? ' class="local-pool-row"' : "";
        return '<tr' + rowClass + '><td style="font-weight: 600;"><span class="pool-key" style="justify-content: center; --key-color: ' + safeColor(row.color) + '"><span style="overflow: hidden; text-overflow: ellipsis; white-space: nowrap;">' + escapeHTML(row.pool) + '</span></span></td><td>' + escapeHTML(row.blocks) + '</td><td>' + escapeHTML(row.work) + '</td><td>' + escapeHTML(row.last) + '</td></tr>';
      }).join("");
    }
    function notesHTML(notes) {
      const rows = Array.isArray(notes) ? notes : [];
      return rows.map((note) => '<div class="muted" style="margin-top: 10px; font-size: 0.82rem;">' + escapeHTML(note) + '</div>').join("");
    }
    async function updateStatusView() {
      if (activeLiveTab() !== "status") return;
      const payload = await liveJSON("/api/live/status");
      setHTML("#status-error", payload.api_error ? '<div class="api-error">' + escapeHTML(payload.api_error) + '</div>' : "");
      setHTML("#overview-warnings", notesHTML(payload.warnings));
      setHTML('[data-live-steps="node"]', statusStepsHTML(payload.node?.status_steps));
      setHTML('[data-live-evidence="node"]', metricCardsHTML(payload.node?.evidence));
      setHTML('[data-live-steps="status"]', statusStepsHTML(payload.status_steps));
      setHTML('[data-live-evidence="status"]', metricCardsHTML([...(payload.evidence || []), ...(payload.secondary || [])]));
      const asicRows = document.getElementById("asic-performance-rows");
      if (asicRows && !asicRows.querySelector(".asic-detail-row:not([hidden])") && !asicRows.contains(document.activeElement)) {
        asicRows.innerHTML = asicPerformanceRowsHTML(payload.rows);
      }
      setHTML("#asic-device-rows", asicDeviceRowsHTML(payload.device_rows));
      if (payload.chart_json) writeASICPerformanceSeries(parseASICPerformanceSeries(payload.chart_json));
    }
    async function updateLogsView() {
      if (activeLiveTab() !== "logs") return;
      const panel = document.querySelector("[data-active-log-panel]");
      const pane = panel?.querySelector(".log-pane");
      const endpoint = panel?.dataset.contentEndpoint;
      if (!panel || !pane || !endpoint) return;
      const nearBottom = pane.scrollHeight - pane.scrollTop - pane.clientHeight < 48;
      const previousTop = pane.scrollTop;
      const response = await fetch(endpoint, {headers: {Accept: "text/plain"}, cache: "no-store"});
      if (!response.ok) throw new Error(endpoint + " returned " + response.status);
      pane.textContent = await response.text();
      if (nearBottom) {
        pane.scrollTop = pane.scrollHeight;
      } else {
        pane.scrollTop = previousTop;
      }
    }
    async function updatePeersView() {
      if (activeLiveTab() !== "peers") return;
      const payload = await liveJSON("/api/live/peers");
      setText("#peers-updated-at", payload.updated_at || "");
      setText("#peers-count", String(payload.peer_count ?? 0));
      setText("#peers-method", payload.method || "getPeerForks");
      setText("#peers-refresh-every", payload.refresh_every || "10m");
      setHTML("#peers-error", payload.api_error ? '<div class="api-error">' + escapeHTML(payload.api_error) + '</div>' : "");
      setHTML("#peer-table-wrap", peerTableHTML(payload));
    }
    async function updateStratumRuntimeView() {
      if (activeLiveTab() !== "stratum-runtime") return;
      const payload = await liveJSON("/api/live/stratum-runtime");
      setHTML("#stratum-runtime-metrics", metricCardsHTML([...(payload.evidence || []), ...(payload.secondary || [])]));
      setHTML("#stratum-client-rows", stratumClientRowsHTML(payload.rows));
    }
    async function updateChainDAGView() {
      if (activeLiveTab() !== "chain-dag") return;
      const payload = await liveJSON("/api/live/chain-dag");
      setHTML("#chain-dag-metrics", chainDAGMetricCardsHTML(payload));
      setHTML("#chain-dag-heading-warning", chainDAGWarningsHTML(payload.warnings));
      setHTML("#chain-dag-rows", chainDAGRowsHTML(payload.rows));
      if (payload.chart_json) writeChainDAGGraph(parseChainDAGGraph(payload.chart_json));
      window.chainDAGLastFullUpdateAt = Date.now();
    }
    async function updatePoolEarningsView() {
      if (activeLiveTab() !== "pool-earnings") return;
      const payload = await liveJSON("/api/live/pool-earnings");
      setHTML("#pool-earnings-metrics", metricCardsHTML(payload.evidence));
      setHTML("#pool-earnings-groups", evidenceGroupsHTML(payload.groups));
      setHTML("#pool-earnings-list", minerEarningsRowsHTML(payload.rows));
    }
	    function applyGlobalPoolEarnings(payload) {
	      if (!payload) return;
	      setText("#global-pool-earnings-title", payload.title);
	      setHTML("#global-pool-earnings-body", globalPoolRowsHTML(payload.rows));
	      setHTML("#global-pool-earnings-notes", notesHTML(payload.notes));
	    }
	    function parseProducerTrendSeries(raw) {
	      try {
	        const parsed = JSON.parse(raw || "[]");
	        return Array.isArray(parsed) ? parsed : [];
	      } catch (error) {
	        return [];
	      }
	    }
	    function currentProducerTrendSeries() {
	      if (!Array.isArray(window.producerTrendSeries)) {
	        const el = document.getElementById("producerTrendData");
	        window.producerTrendSeries = parseProducerTrendSeries(el ? el.textContent : "[]");
	      }
	      return window.producerTrendSeries;
	    }
	    function writeProducerTrendSeries(series) {
	      const bounded = (Array.isArray(series) ? series : []).map((item) => ({
	        ...item,
	        points: Array.isArray(item.points) ? item.points.slice(-1600) : [],
	      }));
	      window.producerTrendSeries = bounded;
	      const el = document.getElementById("producerTrendData");
	      if (el) el.textContent = JSON.stringify(bounded);
	    }
	    function mergeProducerTrendSeries(raw) {
	      const incoming = parseProducerTrendSeries(raw);
	      if (!incoming.length) return;
	      const byLabel = new Map();
	      currentProducerTrendSeries().forEach((series) => {
	        byLabel.set(series.label || "", {...series, points: Array.isArray(series.points) ? series.points.slice() : []});
	      });
	      incoming.forEach((series) => {
	        const key = series.label || "";
	        const current = byLabel.get(key) || {...series, points: []};
	        const points = new Map(current.points.map((point) => [point.t, point]));
	        (Array.isArray(series.points) ? series.points : []).forEach((point) => points.set(point.t, point));
	        current.color = series.color || current.color;
	        current.local = Boolean(series.local || current.local);
	        current.points = Array.from(points.values()).sort((a, b) => Date.parse(a.t) - Date.parse(b.t)).slice(-1600);
	        byLabel.set(key, current);
	      });
	      writeProducerTrendSeries(Array.from(byLabel.values()));
	    }
	    function activeProducerTrendRange() {
	      return window.producerTrendRange || "1h";
	    }
	    function producerTrendRefreshDelay(range) {
	      return 0;
	    }
	    async function loadProducerTrendRange(range, force) {
	      if (activeLiveTab() !== "global") return;
	      const now = Date.now();
	      const delay = producerTrendRefreshDelay(range);
	      if (!force && window.producerTrendLastFetchAt && now - window.producerTrendLastFetchAt < delay) return;
	      window.producerTrendLastFetchAt = now;
	      const payload = await liveJSON("/api/live/global/trend?range=" + encodeURIComponent(range));
	      if (payload.chart_json) {
	        const parsed = parseProducerTrendSeries(payload.chart_json);
	        if (parsed.length || !currentProducerTrendSeries().length) {
	          writeProducerTrendSeries(parsed);
	          window.redrawProducerTrend();
	        }
	      }
	    }
	    window.loadProducerTrendRange = loadProducerTrendRange;
	    async function updateGlobalView() {
	      if (activeLiveTab() !== "global") return;
	      const range = activeProducerTrendRange();
	      const now = Date.now();
	      const includeChart = true;
	      const payload = await liveJSON("/api/live/global?range=" + encodeURIComponent(range) + "&chart=1");
	      if (payload.chart_json) {
	        const parsed = parseProducerTrendSeries(payload.chart_json);
	        if (parsed.length || !currentProducerTrendSeries().length) {
	          writeProducerTrendSeries(parsed);
	          window.producerTrendLastFetchAt = now;
	          window.redrawProducerTrend();
	        }
	      }
	      setHTML("#global-metrics", metricCardsHTML(payload.evidence));
	      applyGlobalPoolEarnings(payload.pool_earnings);
	    }
	    const liveUpdateQueued = {status: false, peers: false, logs: false, poolEarnings: false, global: false, stratumRuntime: false, chainDAG: false};
	    const liveUpdateTimers = {status: 0, peers: 0, logs: 0, poolEarnings: 0, global: 0, stratumRuntime: 0, chainDAG: 0};
	    const liveUpdateLastRun = {status: 0, peers: 0, logs: 0, poolEarnings: 0, global: 0, stratumRuntime: 0, chainDAG: 0};
	    const liveUpdateMinDelay = {status: 500, peers: 600000, logs: 1000, poolEarnings: 750, global: 750, stratumRuntime: 750, chainDAG: 2000};
	    async function runLiveUpdate(kind) {
	      liveUpdateLastRun[kind] = Date.now();
	      if (liveUpdatePending[kind]) {
	        liveUpdateQueued[kind] = true;
	        return;
      }
      liveUpdatePending[kind] = true;
      try {
        if (kind === "status") await updateStatusView();
        if (kind === "peers") await updatePeersView();
        if (kind === "logs") await updateLogsView();
        if (kind === "stratumRuntime") await updateStratumRuntimeView();
        if (kind === "chainDAG") await updateChainDAGView();
        if (kind === "poolEarnings") await updatePoolEarningsView();
        if (kind === "global") await updateGlobalView();
      } catch (error) {
        console.error("live dashboard update failed", error);
        if (kind === "status" && activeLiveTab() === "status") {
          setHTML("#status-error", '<div class="api-error">Live status is unavailable; displayed data may be stale.</div>');
        }
      } finally {
        liveUpdatePending[kind] = false;
        if (liveUpdateQueued[kind]) {
          liveUpdateQueued[kind] = false;
          void runLiveUpdate(kind);
	        }
        if (kind === "peers" && activeLiveTab() === "peers") {
          scheduleLiveUpdate("peers");
        }
	      }
	    }
	    function scheduleLiveUpdate(kind) {
	      const elapsed = Date.now() - (liveUpdateLastRun[kind] || 0);
	      const wait = Math.max(0, (liveUpdateMinDelay[kind] || 500) - elapsed);
	      if (liveUpdateTimers[kind]) return;
	      liveUpdateTimers[kind] = window.setTimeout(() => {
	        liveUpdateTimers[kind] = 0;
	        void runLiveUpdate(kind);
	      }, wait);
	    }
	    window.refreshLiveTab = function () {
	      const tab = activeLiveTab();
      if (tab === "status") void runLiveUpdate("status");
      if (tab === "peers") void runLiveUpdate("peers");
      if (tab === "logs") void runLiveUpdate("logs");
      if (tab === "stratum-runtime") void runLiveUpdate("stratumRuntime");
      if (tab === "chain-dag") void runLiveUpdate("chainDAG");
      if (tab === "pool-earnings") void runLiveUpdate("poolEarnings");
      if (tab === "global") void runLiveUpdate("global");
    };
    function scheduleActiveLiveCatchup() {
      const tab = activeLiveTab();
      if (tab === "status") scheduleLiveUpdate("status");
      if (tab === "peers") scheduleLiveUpdate("peers");
      if (tab === "logs") scheduleLiveUpdate("logs");
      if (tab === "stratum-runtime") scheduleLiveUpdate("stratumRuntime");
      if (tab === "chain-dag") scheduleLiveUpdate("chainDAG");
      if (tab === "pool-earnings") scheduleLiveUpdate("poolEarnings");
      if (tab === "global") scheduleLiveUpdate("global");
    }
    function liveEventPayload(event) {
      try {
        return JSON.parse(event.data || "{}");
      } catch (error) {
        return {};
      }
    }
    function liveEventType(event) {
      return liveEventPayload(event).type || "dashboard";
    }
	    function refreshLiveTabForEvent(event) {
	      const payload = liveEventPayload(event);
	      const type = payload.type || "dashboard";
	      const tab = activeLiveTab();
	      if (tab === "global" && (type === "global" || type === "chain-dag" || type === "chain-live" || type === "stale")) scheduleLiveUpdate("global");
	      if (tab === "status" && ["global", "pool", "asic", "host", "stale", "dashboard"].includes(type)) scheduleLiveUpdate("status");
	      if (tab === "stratum-runtime" && ["pool", "asic", "stale", "dashboard"].includes(type)) scheduleLiveUpdate("stratumRuntime");
	      if (tab === "chain-dag" && type === "chain-dag") {
	        if (payload.full === "true") {
	          scheduleLiveUpdate("chainDAG");
	          return;
	        }
	        const patched = applyChainDAGPatch(payload.patch);
	        if (patched) {
	          window.chainDAGLastPatchAt = Date.now();
	          return;
	        }
	        scheduleLiveUpdate("chainDAG");
	      }
	      if (tab === "chain-dag" && ["stale", "dashboard"].includes(type)) scheduleLiveUpdate("chainDAG");
	      if (tab === "pool-earnings" && ["pool", "stale", "dashboard"].includes(type)) scheduleLiveUpdate("poolEarnings");
	    }
	    let dashboardEvents = null;
	    function connectDashboardEvents() {
	      if (!window.EventSource || dashboardEvents) return;
	      const events = new EventSource("/events");
	      dashboardEvents = events;
	      events.addEventListener("dashboard-update", refreshLiveTabForEvent);
	      events.onerror = () => {
	        scheduleActiveLiveCatchup();
	      };
	    }
	    function closeDashboardEvents() {
	      if (!dashboardEvents) return;
	      dashboardEvents.close();
	      dashboardEvents = null;
	    }
	    connectDashboardEvents();
	    document.addEventListener("visibilitychange", () => {
	      if (document.hidden) {
	        closeDashboardEvents();
	      } else {
	        connectDashboardEvents();
	        scheduleActiveLiveCatchup();
	      }
	    });
	    window.addEventListener("pagehide", closeDashboardEvents);
	    window.refreshLiveTab();
	  </script>
	</body>
</html>
{{end}}
`

const tabTemplates = `
{{define "summary"}}
<div class="center-stack" style="margin-bottom: 16px;">
  <div>
    <div class="eyebrow">{{.Title}}</div>
    <div class="state-badge {{toneClass .StateTone}}">{{.State}}</div>
  </div>
</div>
{{if .APIError}}<div class="api-error" style="margin-bottom: 16px;">{{.APIError}}</div>{{end}}
{{end}}

{{define "status-panel"}}
<section class="panel panel-feature center-stack">
  <div style="width: 100%;">
    <div class="eyebrow">{{.Title}}</div>
    <div class="status-grid" data-live-steps="{{if eq .Title "Node Status"}}node{{else}}status{{end}}">
      {{range .StatusSteps}}
      {{if .Active}}
      <div class="status-chip active">{{.Label}}</div>
      {{else}}
      <div class="status-chip">{{.Label}}</div>
      {{end}}
      {{else}}
      <div class="status-chip active">{{.State}}</div>
      {{end}}
    </div>
  </div>
  <div style="width: 100%;"><div class="metric-grid {{if eq .Title "Node Status"}}node-status-grid{{end}}" data-live-evidence="{{if eq .Title "Node Status"}}node{{else}}status{{end}}">{{template "evidence-cards" .Evidence}}</div></div>
</section>
{{end}}

{{define "metric-panel"}}
<section class="panel panel-feature center-stack">
  <div style="width: 100%; max-width: 920px;">{{template "evidence-grid" .}}</div>
</section>
{{end}}

{{define "status-ladder"}}
<section class="panel panel-feature center-stack">
  <div>
    <div class="eyebrow">{{.Title}}</div>
    <div class="status-grid" data-live-steps="status">
      {{range .StatusSteps}}
      {{if .Active}}
      <div class="status-chip active">{{.Label}}</div>
      {{else}}
      <div class="status-chip">{{.Label}}</div>
      {{end}}
      {{end}}
    </div>
  </div>
  <div style="width: 100%;">
    <div class="metric-grid pool-status-grid compact-metrics-grid" data-live-evidence="status">
      {{template "evidence-cards" .Evidence}}
      {{template "evidence-cards" .Secondary}}
    </div>
  </div>
</section>
{{if .APIError}}<div class="api-error" style="margin-bottom: 16px;">{{.APIError}}</div>{{end}}
{{end}}

{{define "evidence-cards"}}
{{range .}}
<div class="metric-card{{if eq .Label "Pause Reason"}} metric-card-pause-reason metric-card-nowrap{{end}}{{if eq .Label "Pool Endpoint"}} metric-card-endpoint metric-card-nowrap{{end}}">
  <div class="metric-label">{{.Label}}</div>
  <div class="metric-value">{{if .Value}}{{.Value}}{{else}}unknown{{end}}</div>
</div>
{{end}}
{{end}}

{{define "evidence-grid"}}
<div class="metric-grid">
  {{template "evidence-cards" .}}
</div>
{{end}}

{{define "evidence-groups"}}
<div class="stack-lg">
  {{range .}}
  <section class="panel panel-feature center-stack">
    <h2 class="section-title">{{.Title}}</h2>
    <div style="width: 100%; max-width: 920px;">{{template "evidence-grid" .Items}}</div>
  </section>
  {{end}}
</div>
{{end}}

{{define "overview"}}
{{template "status-live" .}}
{{end}}

{{define "status"}}
{{template "status-live" .}}
{{end}}

{{define "status-live"}}
<section id="tab-content" data-live-tab="status">
  <header class="page-header">
    <div>
      <h2 class="page-title">Overview</h2>
      <p class="page-description">Live node, pool, template, and ASIC production state.</p>
    </div>
  </header>
  <div id="status-error">{{if .APIError}}<div class="api-error" style="margin-bottom: 16px;">{{.APIError}}</div>{{end}}</div>
  <div class="stack-lg overview-status-stack">
    {{template "status-panel" .Node}}
    {{template "status-ladder" .}}
  </div>
  <div id="overview-warnings" aria-live="polite">{{range .Warnings}}<div class="muted" style="margin-top: 10px; font-size: 0.82rem;">{{.}}</div>{{end}}</div>
  <div data-live-groups="status">{{template "evidence-groups" .Groups}}</div>
  {{template "asic-performance-panel" .}}
</section>
{{end}}

{{define "miner-table-panel"}}
<section class="panel">
  <h2 class="section-title">Miner Table</h2>
  <div style="margin-top: 12px; overflow-x: auto;">
    <table class="data-table">
      <thead><tr><th>MAC Address</th><th>IP</th><th>Status</th><th class="num">Shares</th><th>Last Share</th></tr></thead>
      <tbody id="status-miner-rows">{{range .Rows}}<tr><td style="font-weight: 600;">{{index . "mac"}}</td><td>{{index . "observed"}}</td><td>{{index . "status"}}</td><td class="num">{{index . "shares"}}</td><td>{{index . "last"}}</td></tr>{{else}}<tr><td class="muted" colspan="5" style="padding: 18px 12px;">No miner rows in API payload.</td></tr>{{end}}</tbody>
    </table>
  </div>
</section>
{{end}}

{{define "peer-table"}}
{{if .Columns}}
<table class="data-table">
  <thead>
    <tr>{{range .Columns}}{{ $column := . }}{{ $hash := peerHashColumn $column }}{{with peerColumnTooltip $column}}<th class="{{if $hash}}hash-cell {{end}}has-tooltip" data-tooltip="{{.}}" tabindex="0">{{$column}}</th>{{else}}<th{{if $hash}} class="hash-cell"{{end}}>{{$column}}</th>{{end}}{{end}}</tr>
  </thead>
  <tbody id="peer-list-rows">
    {{range $row := .Rows}}
    <tr>{{range $.Columns}}{{if peerHashColumn .}}<td class="hash-cell" title="{{index $row .}}"><span class="hash-cell-text">{{index $row .}}</span></td>{{else}}<td>{{index $row .}}</td>{{end}}{{end}}</tr>
    {{else}}
    <tr><td class="muted" colspan="{{len .Columns}}" style="padding: 18px 12px;">No peers returned by peer RPC.</td></tr>
    {{end}}
  </tbody>
</table>
{{else}}
<div class="muted" style="padding: 18px 12px;">No peer columns returned by peer RPC.</div>
{{end}}
{{end}}

{{define "peers"}}
<section id="tab-content" data-live-tab="peers">
  <header class="page-header">
    <div>
      <h2 class="page-title">Peers</h2>
      <p class="page-description">Current native node peers.</p>
    </div>
    <div class="state-badge {{toneClass .StateTone}}">{{.State}}</div>
  </header>
  <section class="panel">
    <div class="toolbar-row" style="margin-bottom: 12px;">
      <h2 class="section-title" style="margin: 0;">Peer List</h2>
      <div class="toolbar-row">
        <span class="muted">Peers: <strong id="peers-count">{{.PeerCount}}</strong></span>
        <span class="muted">RPC: <strong id="peers-method">{{.Method}}</strong></span>
        <span class="muted">Updated: <strong id="peers-updated-at">{{.UpdatedAt}}</strong></span>
        <span class="muted">Poll: <strong id="peers-refresh-every">{{.RefreshEvery}}</strong></span>
      </div>
    </div>
    <div id="peers-error">{{if .APIError}}<div class="api-error">{{.APIError}}</div>{{end}}</div>
    <div id="peer-table-wrap" class="peer-table-scroll">{{template "peer-table" .}}</div>
  </section>
</section>
{{end}}

{{define "asic-performance-panel"}}
<section class="panel">
  <h2 class="section-title">ASIC Performance</h2>
  <script type="application/json" id="asicPerformanceData">{{.ChartJSON}}</script>
  <div class="chart-wrap" style="margin-top: 12px; height: 260px; min-height: 260px;"><canvas id="asicPerformanceChart" style="width: 100%; height: 100%;"></canvas></div>
  <div class="chart-legend" id="asicPerformanceLegend"></div>
  <div style="margin-top: 12px; overflow-x: auto;">
    <table class="data-table">
      <thead><tr><th>MAC Address</th><th>IP</th><th>Status</th><th>Ready</th><th class="num">Hashrate</th><th class="num">Shares</th><th>Job Age</th><th>Last Seen</th><th>Action</th></tr></thead>
      <tbody id="asic-performance-rows">{{range .Rows}}<tr class="asic-summary-row" data-asic-row="{{index . "detail_id"}}"><td style="font-weight: 600;">{{index . "mac"}}</td><td>{{index . "ip"}}</td><td>{{index . "status"}}</td><td>{{index . "ready"}}</td><td class="num">{{index . "hashrate"}}</td><td class="num">{{index . "shares"}}</td><td>{{index . "job_age"}}</td><td>{{index . "last"}}</td><td><span class="asic-row-action"><button type="button" class="ghost-button compact" data-asic-detail-toggle data-asic-detail-target="asic-detail-{{index . "detail_id"}}">Restart</button></span></td></tr><tr id="asic-detail-{{index . "detail_id"}}" class="asic-detail-row" data-detail-key="{{index . "detail_id"}}" hidden><td colspan="9"><div class="asic-detail-panel"><div class="asic-detail-grid"><div class="asic-detail-item"><div class="asic-detail-label">Why not mining / state</div><div class="asic-detail-value">{{index . "diagnostic"}}</div></div><div class="asic-detail-item"><div class="asic-detail-label">Active Pool</div><div class="asic-detail-value">{{index . "active_pool"}}</div></div><div class="asic-detail-item"><div class="asic-detail-label">Pool User</div><div class="asic-detail-value">{{index . "pool_user"}}</div></div><div class="asic-detail-item"><div class="asic-detail-label">Pool Legal</div><div class="asic-detail-value">{{index . "pool_legal"}}</div></div><div class="asic-detail-item"><div class="asic-detail-label">Protocol</div><div class="asic-detail-value">{{index . "protocol"}}</div></div><div class="asic-detail-item"><div class="asic-detail-label">Blocks 30m / 1h / 24h</div><div class="asic-detail-value">{{index . "blocks_30m"}} / {{index . "blocks_1h"}} / {{index . "blocks_24h"}}</div></div><div class="asic-detail-item"><div class="asic-detail-label">Rejects 30m</div><div class="asic-detail-value">{{index . "rejects_30m"}}</div></div><div class="asic-detail-item"><div class="asic-detail-label">Clock / Voltage</div><div class="asic-detail-value">{{index . "clock"}} / {{index . "voltage"}}</div></div><div class="asic-detail-item"><div class="asic-detail-label">Temp / Fan</div><div class="asic-detail-value">{{index . "temperature"}} / {{index . "fan"}}</div></div><div class="asic-detail-item"><div class="asic-detail-label">Telemetry</div><div class="asic-detail-value">{{index . "telemetry"}} / {{index . "telemetry_age"}}</div></div><div class="asic-detail-item"><div class="asic-detail-label">Telemetry Errors</div><div class="asic-detail-value">{{index . "telemetry_errors"}}</div></div><div class="asic-detail-item"><div class="asic-detail-label">Source</div><div class="asic-detail-value">{{index . "source"}}</div></div></div><form class="form-grid" hx-post="/actions/miner-control/restart" hx-target="#asic-control-result-{{index . "detail_id"}}" hx-swap="innerHTML" hx-confirm="Restart ASIC {{index . "mac"}} at {{index . "ip"}} now?"><input type="hidden" name="target" value="{{index . "target"}}"><div class="field"><label>ASIC Admin Password</label><input name="admin_password" type="password" autocomplete="current-password" {{if eq (index . "restart_disabled") "true"}}disabled{{end}}></div><div class="field"><label>Control Token</label><input name="control_token" type="password" autocomplete="off" {{if eq (index . "restart_disabled") "true"}}disabled{{end}}></div><div class="field"><label>Restart</label><button class="ghost-button" type="submit" {{if eq (index . "restart_disabled") "true"}}disabled{{end}}>Restart ASIC</button></div></form><div class="muted">{{index . "restart_reason"}}</div><section id="asic-control-result-{{index . "detail_id"}}"></section></div></td></tr>{{else}}<tr><td class="muted" colspan="9" style="padding: 18px 12px;">No ASIC performance rows yet.</td></tr>{{end}}</tbody>
    </table>
  </div>
</section>
{{end}}

{{define "asic-device-panel"}}
<section class="panel">
  <h2 class="section-title">ASIC Device Configuration Snapshot</h2>
  <div class="muted" style="margin-top: 6px; font-size: 0.82rem;">Read-only port 80 telemetry. Frequency and voltage are parsed from the active power-plan strings; write controls remain disabled until endpoint payloads are proven.</div>
  <div style="margin-top: 12px; overflow-x: auto;">
    <table class="data-table">
      <thead><tr><th>MAC Address</th><th>IP</th><th>Model</th><th>Firmware</th><th>Clock</th><th>Voltage</th><th>Chips</th><th>Temp</th><th>Fan</th><th>Hashrate</th><th>Power Plan</th><th>Active Pool</th><th>Pool User</th><th>Legal</th><th>Miner Status</th><th class="num">Accepted</th><th class="num">HW Errors</th><th>Telemetry</th></tr></thead>
      <tbody id="asic-device-rows">{{range .DeviceRows}}<tr><td style="font-weight: 600;">{{index . "mac"}}</td><td>{{index . "ip"}}</td><td>{{index . "model"}}</td><td>{{index . "firmware"}}</td><td>{{index . "clock"}}</td><td>{{index . "voltage"}}</td><td>{{index . "chips"}}</td><td>{{index . "temp"}}</td><td>{{index . "fan"}}</td><td>{{index . "hashrate"}}</td><td>{{index . "powerplan"}}</td><td>{{index . "active_pool"}}</td><td>{{index . "pool_user"}}</td><td>{{index . "pool_legal"}}</td><td>{{index . "miner_status"}}</td><td class="num">{{index . "accepted"}}</td><td class="num">{{index . "hw_errors"}}</td><td>{{index . "telemetry"}} / {{index . "telemetry_age"}}</td></tr>{{else}}<tr><td class="muted" colspan="18" style="padding: 18px 12px;">No ASIC device telemetry yet.</td></tr>{{end}}</tbody>
    </table>
  </div>
</section>
{{end}}

{{define "warnings"}}
<section id="tab-content">
  <header class="page-header">
    <div>
      <h2 class="page-title">Warnings</h2>
      <p class="page-description">Active stack, pool, and ASIC fleet warning signals.</p>
    </div>
  </header>
  <section class="panel">
    <h2 class="section-title">Active Warnings</h2>
    {{if .Warnings}}
    <ul class="warning-list">{{range .Warnings}}<li>{{.}}</li>{{end}}</ul>
    {{else}}
    <div class="muted">No active warning rows in the current status payload.</div>
    {{end}}
  </section>
  {{if .Notes}}
  <section class="panel">
    <h2 class="section-title">Notes</h2>
    <ul class="warning-list">{{range .Notes}}<li>{{.}}</li>{{end}}</ul>
  </section>
  {{end}}
</section>
{{end}}

{{define "asic-fleet"}}
<section id="tab-content" data-live-tab="status">
  <header class="page-header">
    <div>
      <h2 class="page-title">ASIC Fleet</h2>
      <p class="page-description">MAC-first miner identity, readiness, protocol, and block production.</p>
    </div>
  </header>
  {{template "asic-performance-panel" .}}
  {{template "asic-device-panel" .}}
</section>
<script>window.requestAnimationFrame(drawASICPerformance);</script>
{{end}}

{{define "stratum-runtime"}}
<section id="tab-content" data-live-tab="stratum-runtime">
  <header class="page-header">
    <div>
      <h2 class="page-title">Stratum Runtime</h2>
      <p class="page-description">Per-lane job readiness, template sequence, backend, and submit-path evidence.</p>
    </div>
  </header>
  {{template "summary" .}}
  <section class="panel">
    <h2 class="section-title">Runtime Contract</h2>
    <div id="stratum-runtime-metrics" class="metric-grid pool-status-grid compact-metrics-grid" style="margin-top: 12px;">
      {{template "evidence-cards" .Evidence}}
      {{template "evidence-cards" .Secondary}}
    </div>
  </section>
  <section class="panel">
    <h2 class="section-title">Miner Lane Jobs</h2>
    <div style="margin-top: 12px; overflow-x: auto;">
      <table class="data-table">
        <thead><tr><th>MAC Address</th><th>IP</th><th>Ready</th><th>Reason</th><th>Job ID</th><th>Job Age</th><th>Seq</th><th>Backend</th><th>Parent</th><th class="num">PDiff</th><th class="num">Expired</th><th class="num">Low Diff</th><th>Subscribed</th><th>Extranonce</th></tr></thead>
        <tbody id="stratum-client-rows">{{range .Rows}}<tr><td style="font-weight: 600;">{{index . "mac"}}</td><td>{{index . "ip"}}</td><td>{{index . "ready"}}</td><td>{{index . "reason"}}</td><td>{{index . "job_id"}}</td><td>{{index . "job_age"}}</td><td>{{index . "seq"}}</td><td>{{index . "backend"}}</td><td>{{index . "parent"}}</td><td class="num">{{index . "pdiff"}}</td><td class="num">{{index . "expired"}}</td><td class="num">{{index . "low_diff"}}</td><td>{{index . "subscribed"}}</td><td>{{index . "protocol"}}</td></tr>{{else}}<tr><td class="muted" colspan="14" style="padding: 18px 12px;">No Stratum clients in job-state snapshot.</td></tr>{{end}}</tbody>
      </table>
    </div>
  </section>
</section>
{{end}}

{{define "chain-dag"}}
<section id="tab-content" data-live-tab="chain-dag">
  <header class="page-header">
    <div>
      <h2 class="page-title">Chain DAG</h2>
    </div>
  </header>
  {{if .APIError}}<div class="api-error" style="margin-bottom: 16px;">{{.APIError}}</div>{{end}}
  <section class="panel">
    <div class="chain-dag-heading-row">
      <h2 class="section-title" style="margin: 0;">Topology Window</h2>
      <div id="chain-dag-heading-warning" class="chain-dag-heading-warning">{{range $idx, $warning := .Warnings}}{{if $idx}} · {{end}}{{$warning}}{{end}}</div>
    </div>
    <div id="chain-dag-metrics" class="metric-grid chain-dag-metrics" style="margin-top: 12px;">
      <div class="{{toneClass .StateTone}} chain-dag-state">{{.State}}</div>
      {{template "evidence-cards" .Evidence}}
    </div>
    <script type="application/json" id="chainDAGData">{{.ChartJSON}}</script>
    <div class="chart-wrap chain-dag-wrap" style="margin-top: 12px;"><canvas id="chainDAGCanvas" style="width: 100%; height: 100%;"></canvas></div>
  </section>
  <section class="panel">
    <h2 class="section-title">Recent Blocks</h2>
    <div style="margin-top: 12px; overflow-x: auto;">
      <table class="data-table">
        <thead><tr><th class="num">Height</th><th>Hash</th><th>Parent</th><th>Producer</th><th>Local</th><th class="num">Parents</th><th class="num">Missing</th><th>Time</th></tr></thead>
        <tbody id="chain-dag-rows">{{range .Rows}}<tr><td class="num">{{index . "height"}}</td><td style="font-weight: 600;">{{index . "hash"}}</td><td>{{index . "parent"}}</td><td>{{index . "producer"}}</td><td>{{index . "local"}}</td><td class="num">{{index . "parents"}}</td><td class="num">{{index . "missing"}}</td><td>{{index . "time"}}</td></tr>{{else}}<tr><td class="muted" colspan="8" style="padding: 18px 12px;">No chain topology rows yet.</td></tr>{{end}}</tbody>
      </table>
    </div>
  </section>
</section>
<script>window.requestAnimationFrame(drawChainDAG);</script>
{{end}}

{{define "control-action-result"}}
<div class="action-result {{.Tone}}">
  <h3 class="section-title" style="margin: 0;">{{.Title}}</h3>
  {{if .Message}}<div class="muted" style="margin-top: 6px;">{{.Message}}</div>{{end}}
  {{if .Items}}<div style="margin-top: 12px;">{{template "evidence-grid" .Items}}</div>{{end}}
</div>
{{end}}

{{define "pool-management"}}
<section id="tab-content">
  <header class="page-header">
    <div>
      <h2 class="page-title">Pool Management</h2>
      <p class="page-description">Pool endpoint, worker identity, control targets, and gated ASIC actions.</p>
    </div>
  </header>
  {{template "summary" .}}
  {{if .Evidence}}<section class="panel"><h2 class="section-title">Pool Runtime</h2><div style="margin-top: 12px;">{{template "evidence-grid" .Evidence}}</div></section>{{end}}
  {{if .DisabledReason}}<div class="api-error" style="margin-bottom: 16px;">{{.DisabledReason}}</div>{{end}}
  <section id="minerControlResult"></section>
  <section class="panel">
    <h2 class="section-title">Apply Pool Configuration</h2>
    <form class="stack-md" style="margin-top: 12px;" hx-post="/actions/miner-control/configure" hx-target="#minerControlResult" hx-swap="innerHTML" hx-confirm="Apply this pool configuration to the selected miner?">
      <div class="form-grid">
        <div class="field">
          <label>Known Miner</label>
          <select name="target" {{if not .Enabled}}disabled{{end}}>
            {{range .Rows}}<option value="{{index . "target"}}">{{index . "label"}} | {{index . "ip"}} | {{index . "status"}}</option>{{end}}
          </select>
        </div>
        <div class="field"><label>Pool URL</label><input name="pool_url" value="{{.DefaultPoolURL}}" autocomplete="off" {{if not .Enabled}}disabled{{end}}></div>
        <div class="field"><label>Worker / Wallet</label><input name="worker_user" value="{{.DefaultWorkerUser}}" autocomplete="off" {{if not .Enabled}}disabled{{end}}></div>
        <div class="field"><label>Pool Password</label><input name="pool_password" type="password" placeholder="Leave blank for the configured default" autocomplete="off" {{if not .Enabled}}disabled{{end}}></div>
        <div class="field"><label>ASIC Admin Password</label><input name="admin_password" type="password" autocomplete="current-password" {{if not .Enabled}}disabled{{end}}></div>
        <div class="field"><label>Control Token</label><input name="control_token" type="password" autocomplete="off" {{if not .Enabled}}disabled{{end}}></div>
        <label class="check-field"><input type="checkbox" name="replace_existing" value="true" {{if .ReplaceExisting}}checked{{end}} {{if not .Enabled}}disabled{{end}}> Replace other pool entries</label>
      </div>
      <button class="ghost-button" type="submit" {{if not .Enabled}}disabled{{end}}>Apply Pool Config</button>
    </form>
  </section>
  <section class="panel">
    <h2 class="section-title">Restart Miner</h2>
    <form class="stack-md" style="margin-top: 12px;" hx-post="/actions/miner-control/restart" hx-target="#minerControlResult" hx-swap="innerHTML" hx-confirm="Restart the selected miner now?">
      <div class="form-grid">
        <div class="field">
          <label>Known Miner</label>
          <select name="target" {{if not .Enabled}}disabled{{end}}>
            {{range .Rows}}<option value="{{index . "target"}}">{{index . "label"}} | {{index . "ip"}} | {{index . "status"}}</option>{{end}}
          </select>
        </div>
        <div class="field"><label>ASIC Admin Password</label><input name="admin_password" type="password" autocomplete="current-password" {{if not .Enabled}}disabled{{end}}></div>
        <div class="field"><label>Control Token</label><input name="control_token" type="password" autocomplete="off" {{if not .Enabled}}disabled{{end}}></div>
      </div>
      <button class="ghost-button" type="submit" {{if not .Enabled}}disabled{{end}}>Restart Miner</button>
    </form>
  </section>
</section>
{{end}}

{{define "miner-control"}}
{{template "pool-management" .}}
{{end}}

{{define "log-tab"}}
<section id="tab-content" class="log-panel" data-live-tab="logs">
  {{template "log-panel" .}}
</section>
{{end}}

{{define "logs"}}
<section id="tab-content" class="log-panel" data-live-tab="logs">
  <header class="page-header" style="margin-bottom: 16px;">
    <div>
      <h2 class="page-title">Logs</h2>
      <p class="page-description">Node, pool, and dashboard logs from the local runtime containers.</p>
    </div>
  </header>
  <div class="log-tabs" role="tablist" aria-label="Log source">
    <button type="button" class="range-button active" data-log-tab data-log-title="{{.Node.Title}}" data-log-endpoint="{{.Node.ContentEndpoint}}">Node</button>
    <button type="button" class="range-button" data-log-tab data-log-title="{{.Pool.Title}}" data-log-endpoint="{{.Pool.ContentEndpoint}}">Pool</button>
    <button type="button" class="range-button" data-log-tab data-log-title="{{.Dashboard.Title}}" data-log-endpoint="{{.Dashboard.ContentEndpoint}}">Dashboard</button>
  </div>
  <section class="panel log-panel log-shell" data-active-log-panel data-content-endpoint="{{.Node.ContentEndpoint}}">
    <div class="toolbar-row" style="margin-bottom: 12px;">
      <h2 class="section-title" style="margin: 0;" data-active-log-title>{{.Node.Title}}</h2>
      <div class="toolbar-row">
        <div class="muted" style="font-size: 0.82rem;">tailing latest lines</div>
        <button type="button" data-copy-logs class="ghost-button">Copy logs</button>
      </div>
    </div>
    <pre class="log-pane">{{.Node.Content}}</pre>
  </section>
</section>
{{end}}

{{define "log-panel"}}
<section class="panel log-panel log-shell" id="{{if eq .Endpoint "/logs/node"}}node-log-panel{{else}}pool-log-panel{{end}}" data-active-log-panel data-content-endpoint="{{.ContentEndpoint}}">
  <div class="toolbar-row" style="margin-bottom: 12px;">
    <h2 class="section-title" style="margin: 0;">{{.Title}}</h2>
    <div class="toolbar-row">
      <div class="muted" style="font-size: 0.82rem;">tailing latest lines</div>
      <button type="button" data-copy-logs class="ghost-button">Copy logs</button>
    </div>
  </div>
  <pre class="log-pane">{{.Content}}</pre>
</section>
{{end}}

{{define "pool-earnings"}}
<section id="tab-content" data-live-tab="pool-earnings">
  <section class="panel panel-feature center-stack"><div style="width: 100%; max-width: 920px;"><div id="pool-earnings-metrics" class="metric-grid">{{template "evidence-cards" .Evidence}}</div></div></section>
  <div id="pool-earnings-groups" class="stack-lg">{{range .Groups}}<section class="panel panel-feature center-stack"><h2 class="section-title">{{.Title}}</h2><div style="width: 100%; max-width: 920px;">{{template "evidence-grid" .Items}}</div></section>{{end}}</div>
  <section class="panel">
    <h2 class="section-title">Miner Earnings List</h2>
    <div class="stack-md" style="margin-top: 12px;">
      <div class="row-card-head"><div>Wallet</div><div style="text-align: right;">Avg</div><div style="text-align: right;">Total</div></div>
      <div id="pool-earnings-list" class="stack-md">{{range .Rows}}<div class="row-card"><div style="overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-weight: 600;">{{index . "wallet"}}</div><div style="text-align: right; font-weight: 700;">{{index . "avg"}} BDAG/h</div><div style="text-align: right;" class="muted">{{index . "total"}} BDAG</div></div>{{else}}<div class="muted">No miner earnings rows in API payload.</div>{{end}}</div>
    </div>
  </section>
</section>
{{end}}

{{define "global"}}
<section id="tab-content" data-live-tab="global">
  <section class="panel panel-feature">
    <div class="producer-trend-toolbar">
      <h2 class="section-title" style="margin: 0;">Producer Trend</h2>
      <div class="range-row producer-trend-metrics">
        <button type="button" class="range-button active" data-metric="blocks" onclick="setProducerTrendMetric('blocks')">Blocks</button>
        <button type="button" class="range-button" data-metric="bdag" onclick="setProducerTrendMetric('bdag')">BDAG</button>
        <button type="button" class="range-button" data-metric="usd" onclick="setProducerTrendMetric('usd')">USD</button>
      </div>
      <div class="range-row producer-trend-ranges">
        <button type="button" class="range-button active" data-range="1h" onclick="setProducerTrendRange('1h')">1h</button>
        <button type="button" class="range-button" data-range="4h" onclick="setProducerTrendRange('4h')">4h</button>
        <button type="button" class="range-button" data-range="12h" onclick="setProducerTrendRange('12h')">12h</button>
        <button type="button" class="range-button" data-range="24h" onclick="setProducerTrendRange('24h')">24h</button>
        <button type="button" class="range-button" data-range="3d" onclick="setProducerTrendRange('3d')">3d</button>
        <button type="button" class="range-button" data-range="7d" onclick="setProducerTrendRange('7d')">7d</button>
        <button type="button" class="range-button" data-range="1month" onclick="setProducerTrendRange('1month')">1mo</button>
      </div>
    </div>
    <div id="producerTrendRangeStatus" class="muted" style="margin-top: 10px; font-size: 0.82rem;"></div>
    <div class="chart-wrap" style="margin-top: 12px;"><canvas id="producerTrend" style="width: 100%; height: 100%;"></canvas></div>
    <div class="chart-legend" id="producerTrendLegend"></div>
    <script type="application/json" id="producerTrendData">{{.ChartJSON}}</script>
  </section>
  {{template "global-pool-earnings-placeholder" .}}
  <section class="panel global-metrics-panel"><div id="global-metrics" class="metric-grid global-metrics-grid">{{template "evidence-cards" .Evidence}}</div></section>
  <script>
    window.redrawProducerTrend = window.redrawProducerTrend || function () {
      requestAnimationFrame(() => {
        try {
          if (window.drawProducerTrend) window.drawProducerTrend();
        } catch (error) {
          console.error("producer trend redraw failed", error);
        }
      });
    };
    window.producerTrendRanges = window.producerTrendRanges || {
      "1h": {hours: 1, label: "1h"},
      "4h": {hours: 4, label: "4h"},
      "12h": {hours: 12, label: "12h"},
      "24h": {hours: 24, label: "24h"},
      "3d": {hours: 72, label: "3d"},
      "7d": {hours: 168, label: "7d"},
      "1month": {hours: 720, label: "1 month"},
    };
    window.producerTrendRange = window.producerTrendRange || "1h";
    window.producerTrendMetric = window.producerTrendMetric || "blocks";
    window.setProducerTrendRange = function (range) {
      window.producerTrendRange = window.producerTrendRanges[range] ? range : "1h";
      document.querySelectorAll(".range-button[data-range]").forEach((button) => {
        button.classList.toggle("active", button.dataset.range === window.producerTrendRange);
      });
      if (window.loadProducerTrendRange) void window.loadProducerTrendRange(window.producerTrendRange, true);
    };
    window.setProducerTrendMetric = function (metric) {
      window.producerTrendMetric = metric;
      document.querySelectorAll(".range-button[data-metric]").forEach((button) => {
        button.classList.toggle("active", button.dataset.metric === metric);
      });
      window.redrawProducerTrend();
    };
	    window.drawProducerTrend = function () {
	      const el = document.getElementById("producerTrendData");
	      const canvas = document.getElementById("producerTrend");
	      const legend = document.getElementById("producerTrendLegend");
	      if (!el || !canvas) return;
	      const series = currentProducerTrendSeries();
      const rect = canvas.parentElement.getBoundingClientRect();
      const ratio = window.devicePixelRatio || 1;
      const width = Math.max(360, Math.floor(rect.width));
      const height = Math.max(260, Math.floor(rect.height));
      canvas.width = width * ratio; canvas.height = height * ratio;
      const ctx = canvas.getContext("2d");
      ctx.setTransform(1, 0, 0, 1, 0, 0);
      ctx.globalAlpha = 1;
      ctx.globalCompositeOperation = "source-over";
      ctx.shadowColor = "transparent";
      ctx.shadowBlur = 0;
      ctx.setLineDash([]);
      ctx.clearRect(0, 0, canvas.width, canvas.height);
      ctx.setTransform(ratio, 0, 0, ratio, 0, 0);
      ctx.globalAlpha = 1;
      ctx.globalCompositeOperation = "source-over";
      ctx.shadowColor = "transparent";
      ctx.shadowBlur = 0;
      ctx.setLineDash([]);
      ctx.fillStyle = "#171421"; ctx.fillRect(0, 0, width, height);
      const parsed = series.map(s => ({...s, points: s.points.map(p => ({...p, t: Date.parse(p.t)})).filter(p => Number.isFinite(p.t))}));
      const allTimes = parsed.flatMap(s => s.points.map(p => p.t)).filter(Number.isFinite);
      const latest = Math.max(...allTimes);
      const earliest = Math.min(...allTimes);
      const coverageHours = Number.isFinite(latest) && Number.isFinite(earliest) ? (latest - earliest) / (60 * 60 * 1000) : 0;
      const rangeInfo = (window.producerTrendRanges || {})[window.producerTrendRange || "1h"] || {hours: 1, label: "1h"};
      const rangeHours = Number(rangeInfo.hours) || 1;
      const metric = window.producerTrendMetric || "blocks";
      const axisMaxT = Math.max(Date.now(), Number.isFinite(latest) ? latest : Date.now());
      const axisMinT = axisMaxT - rangeHours * 60 * 60 * 1000;
      document.querySelectorAll(".range-button[data-range]").forEach((button) => {
        button.disabled = false;
        button.classList.toggle("active", button.dataset.range === (window.producerTrendRange || "1h"));
      });
      document.querySelectorAll(".range-button[data-metric]").forEach((button) => {
        button.classList.toggle("active", button.dataset.metric === metric);
      });
      const status = document.getElementById("producerTrendRangeStatus");
      if (status) {
        const covered = coverageHours >= 48 ? (coverageHours / 24).toFixed(1) + " days" : coverageHours.toFixed(1) + " hours";
        const bestEffortPoints = parsed.flatMap((s) => s.points).filter((point) => point.best_effort);
        let suffix = " Longer ranges fill from persistent Redis buckets as backfill catches up.";
        if (bestEffortPoints.length) {
          bestEffortPoints.sort((a, b) => a.t - b.t);
          const live = bestEffortPoints[bestEffortPoints.length - 1];
          const fetched = Number(live.fetched_blocks || 0);
          const requested = Number(live.requested_blocks || 0);
          const coverage = requested > 0 ? Math.round((fetched / requested) * 100) : Math.round(Number(live.coverage || 0) * 100);
          suffix = " Live Redis view is best-effort and healing " + (fetched || "?") + "/" + (requested || "?") + " blocks" + (coverage ? " (" + coverage + "%)." : ".");
        }
        status.textContent = "Producer history coverage: " + covered + " for " + rangeInfo.label + "." + suffix;
      }
      const windowed = parsed.map(s => ({...s, points: s.points.filter(p => p.t >= axisMinT && p.t <= axisMaxT + 1000 && Number(p[metric]) > 0)})).filter(s => s.points.length);
      const points = windowed.flatMap(s => s.points);
      if (!points.length) {
        if (legend) legend.innerHTML = "";
        ctx.fillStyle = "#b8b0c7";
        ctx.font = "12px Inter, sans-serif";
        ctx.fillText("No producer trend history available yet.", 16, 28);
        return;
      }
      const maxV = Math.max(...points.map(p => Number(p[metric]) || 0), 1);
      const scaleMax = niceAxisMax(maxV);
      const m = {l: 76, r: 28, t: 32, b: 58}, w = width - m.l - m.r, h = height - m.t - m.b;
      ctx.strokeStyle = "#2d2938"; ctx.lineWidth = 1;
      ctx.fillStyle = "#b8b0c7"; ctx.font = "12px Inter, sans-serif";
      const metricLabel = metric === "usd" ? "USD/h" : metric === "bdag" ? "BDAG/h" : "Blocks/h";
      ctx.fillText(metricLabel, m.l, 18);
      ctx.textAlign = "right";
      ctx.textBaseline = "middle";
      for (let i = 0; i <= 4; i++) {
        const y = m.t + h * i / 4;
        const tickValue = scaleMax - (scaleMax * i / 4);
        ctx.strokeStyle = "#2d2938";
        ctx.beginPath(); ctx.moveTo(m.l, y); ctx.lineTo(width - m.r, y); ctx.stroke();
        ctx.fillStyle = "#b8b0c7";
        ctx.fillText(formatYAxisValue(tickValue, metric), m.l - 8, y);
      }
      const xTicks = producerTrendXTicks(axisMinT, axisMaxT, rangeHours);
      ctx.textAlign = "center";
      ctx.textBaseline = "top";
      ctx.font = "11px Inter, sans-serif";
      xTicks.forEach((tick) => {
        const x = m.l + ((tick.t - axisMinT) / Math.max(1, axisMaxT - axisMinT)) * w;
        ctx.strokeStyle = "#2d2938";
        ctx.beginPath(); ctx.moveTo(x, m.t); ctx.lineTo(x, m.t + h + 6); ctx.stroke();
        ctx.fillStyle = "#b8b0c7";
        ctx.fillText(tick.label, x, m.t + h + 12);
      });
      ctx.textAlign = "left";
      ctx.textBaseline = "alphabetic";
      if (legend) {
        legend.innerHTML = "";
        windowed.forEach((s) => {
          const key = document.createElement("span");
          key.className = "legend-key";
          key.classList.toggle("local-pool-highlight", Boolean(s.local));
          key.style.setProperty("--key-color", s.color || "#2bd4a3");
          key.textContent = s.label || "unknown";
          legend.appendChild(key);
        });
      }
      windowed.forEach((s) => {
        const pts = s.points;
        if (!pts.length) return;
        const color = s.color || "#2bd4a3";
        const plotted = pts.map((p) => ({
          x: m.l + ((p.t - axisMinT) / Math.max(1, axisMaxT - axisMinT)) * w,
          y: m.t + h - ((Number(p[metric]) || 0) / scaleMax) * h,
          bestEffort: Boolean(p.best_effort),
        }));
        ctx.lineCap = "round"; ctx.lineJoin = "round";
        if (s.local) {
          ctx.save();
          ctx.strokeStyle = color;
          ctx.globalAlpha = 0.42;
          ctx.lineWidth = 10;
          ctx.shadowColor = color;
          ctx.shadowBlur = 18;
          ctx.beginPath();
          drawSmoothPath(ctx, plotted);
          ctx.stroke();
          ctx.restore();
        }
        ctx.strokeStyle = color; ctx.lineWidth = s.local ? 4 : 3; ctx.beginPath();
        drawSmoothPath(ctx, plotted);
        ctx.stroke();
        if (plotted.length === 1) {
          const point = plotted[0];
          const span = 10;
          ctx.beginPath();
          ctx.moveTo(Math.max(m.l, point.x - span), point.y);
          ctx.lineTo(Math.min(width - m.r, point.x + span), point.y);
          ctx.stroke();
        }
      });
    };
    function formatYAxisValue(value, metric) {
      const n = Number(value) || 0;
      if (metric === "usd") {
        if (n >= 1) return "$" + n.toFixed(2);
        if (n >= 0.01) return "$" + n.toFixed(3);
        return "$" + n.toFixed(4);
      }
      if (metric === "bdag") {
        if (n >= 1000) return Math.round(n / 1000) + "k";
        return Math.round(n).toString();
      }
      if (n >= 1000) return Math.round(n / 1000) + "k";
      if (n >= 10) return Math.round(n).toString();
      return n.toFixed(1);
    }
    window.redrawProducerTrend();
  </script>
</section>
{{end}}

{{define "global-pool-earnings-placeholder"}}
<section id="global-pool-earnings" class="panel" hx-get="/partials/global-pool-earnings" hx-trigger="load" hx-swap="outerHTML">
  <h2 class="section-title" id="global-pool-earnings-title">Estimated Earnings By Pool</h2>
  <div style="margin-top: 12px; overflow-x: auto;">
    <table class="data-table" style="text-align: center;">
      <thead><tr><th>Pool</th><th>Blocks</th><th>Work %</th><th>Last Seen</th></tr></thead>
      <tbody id="global-pool-earnings-body"><tr><td class="muted" colspan="4" style="padding: 18px 12px;">Loading pool earnings scan...</td></tr></tbody>
    </table>
  </div>
  <div id="global-pool-earnings-notes"></div>
</section>
{{end}}

{{define "global-pool-earnings"}}
<section id="global-pool-earnings" class="panel">
  <h2 class="section-title" id="global-pool-earnings-title">{{.Title}}</h2>
  {{if .APIError}}<div class="api-error" style="margin-top: 12px;">{{.APIError}}</div>{{end}}
  <div style="margin-top: 12px; overflow-x: auto;">
    <table class="data-table" style="text-align: center;">
      <thead><tr><th>Pool</th><th>Blocks</th><th>Work %</th><th>Last Seen</th></tr></thead>
      <tbody id="global-pool-earnings-body">{{range .Rows}}<tr class="{{if eq (index . "local") "true"}}local-pool-row{{end}}"><td style="font-weight: 600;"><span class="pool-key" style="justify-content: center; --key-color: {{index . "color"}}"><span style="overflow: hidden; text-overflow: ellipsis; white-space: nowrap;">{{index . "pool"}}</span></span></td><td>{{index . "blocks"}}</td><td>{{index . "work"}}</td><td>{{index . "last"}}</td></tr>{{else}}<tr><td class="muted" colspan="4" style="padding: 18px 12px;">No global producer rows in API payload.</td></tr>{{end}}</tbody>
    </table>
  </div>
  <div id="global-pool-earnings-notes">{{range .Notes}}<div class="muted" style="margin-top: 10px; font-size: 0.82rem;">{{.}}</div>{{end}}</div>
</section>
{{end}}
`
