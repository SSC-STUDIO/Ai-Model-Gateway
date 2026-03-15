package server

import (
	"net/http"
	"strings"
)

const adminIconSVG = `<svg width="256" height="256" viewBox="0 0 96 96" fill="none" xmlns="http://www.w3.org/2000/svg"><rect width="96" height="96" rx="24" fill="#0B0C0C"/><path d="M24 68V28H38L48 52L58 28H72V68H62V46L54 66H42L34 46V68H24Z" fill="#7EE7D6"/><circle cx="73" cy="24" r="8" fill="#F1B866"/></svg>`

const adminHTMLTemplate = `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta name="theme-color" content="#0b0c0c">
  <title>AI Gateway Admin</title>
  <link rel="icon" type="image/svg+xml" href="/favicon.svg">
  <link rel="shortcut icon" href="/favicon.ico">
  <style>
    :root {
      --bg: #070706;
      --bg-2: #0b0b09;
      --ink: #f7f3ee;
      --muted: #b4a99a;
      --panel: rgba(18, 17, 15, 0.82);
      --panel-strong: rgba(24, 22, 19, 0.96);
      --line: rgba(255, 244, 230, 0.14);
      --accent: #7ee7d6;
      --accent-strong: #9af2e5;
      --amber: #f1b866;
      --danger: #ff7f6e;
      --ok-bg: rgba(121, 230, 215, 0.16);
      --danger-bg: rgba(255, 127, 110, 0.18);
      --shadow: 0 28px 62px rgba(0, 0, 0, 0.5);
      --shadow-soft: 0 16px 36px rgba(0, 0, 0, 0.32);
      --glow: 0 0 0 1px rgba(126, 231, 214, 0.1), 0 12px 40px rgba(121, 230, 215, 0.14);
      --page-gutter: clamp(14px, 1.8vw, 36px);
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      min-height: 100vh;
      overflow-x: hidden;
      font-family: "Aptos", "Segoe UI", "PingFang SC", "Noto Sans SC", sans-serif;
      color: var(--ink);
      background: radial-gradient(1200px 700px at 15% -10%, rgba(126, 231, 214, 0.1), transparent 62%),
        radial-gradient(900px 700px at 95% 5%, rgba(241, 184, 102, 0.12), transparent 58%),
        linear-gradient(160deg, var(--bg), var(--bg-2) 40%, #050504 100%);
      background-color: var(--bg);
      position: relative;
    }
    body::before {
      content: "";
      position: fixed;
      inset: 0;
      pointer-events: none;
      background-image:
        radial-gradient(circle at 20% 20%, rgba(255, 255, 255, 0.04), transparent 45%),
        radial-gradient(circle at 80% 0%, rgba(255, 255, 255, 0.03), transparent 40%),
        linear-gradient(120deg, rgba(255, 255, 255, 0.02), transparent 50%);
      opacity: 0.9;
      z-index: 0;
    }
    body::after {
      content: "";
      position: fixed;
      inset: 0;
      pointer-events: none;
      background-image:
        linear-gradient(0deg, rgba(255, 255, 255, 0.025) 1px, transparent 1px),
        linear-gradient(90deg, rgba(255, 255, 255, 0.02) 1px, transparent 1px);
      background-size: 56px 56px;
      opacity: 0.18;
      z-index: 0;
    }
    .wrap {
      width: calc(100vw - (var(--page-gutter) * 2));
      max-width: none;
      margin: clamp(14px, 1.8vw, 24px) auto clamp(20px, 4vw, 56px);
      position: relative;
      z-index: 1;
    }
    .topbar {
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 16px;
      margin-bottom: 12px;
      padding: 10px 14px;
      border: 1px solid rgba(255, 244, 230, 0.12);
      border-radius: 18px;
      background: rgba(10, 10, 9, 0.72);
      box-shadow: var(--shadow-soft);
      backdrop-filter: blur(16px) saturate(120%);
    }
    .brand {
      display: inline-flex;
      align-items: center;
      gap: 12px;
      min-width: 0;
    }
    .brand-mark {
      width: 42px;
      height: 42px;
      border-radius: 14px;
      display: grid;
      place-items: center;
      background: linear-gradient(145deg, rgba(126, 231, 214, 0.18), rgba(241, 184, 102, 0.16));
      border: 1px solid rgba(255, 244, 230, 0.12);
      box-shadow: inset 0 0 0 1px rgba(255,255,255,0.03);
      flex: 0 0 auto;
    }
    .brand-mark svg {
      width: 26px;
      height: 26px;
    }
    .brand-copy {
      min-width: 0;
    }
    .brand-title {
      font-size: 14px;
      font-weight: 800;
      letter-spacing: 0.08em;
      text-transform: uppercase;
    }
    .brand-subtitle {
      margin-top: 2px;
      color: var(--muted);
      font-size: 12px;
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
    }
    .topnav {
      display: flex;
      align-items: center;
      gap: 8px;
      flex-wrap: wrap;
      justify-content: flex-end;
    }
    .topnav a {
      display: inline-flex;
      align-items: center;
      padding: 7px 12px;
      border-radius: 999px;
      border: 1px solid var(--line);
      background: rgba(255,255,255,0.04);
      color: var(--muted);
      font-size: 12px;
      text-decoration: none;
      transition: border-color 140ms ease, color 140ms ease, background 140ms ease;
    }
    .topnav a:hover {
      color: var(--ink);
      border-color: rgba(126, 231, 214, 0.4);
      text-decoration: none;
    }
    .hero {
      display: grid;
      grid-template-columns: 1.3fr 0.7fr;
      gap: clamp(14px, 1.4vw, 20px);
      margin-bottom: clamp(14px, 1.6vw, 20px);
    }
    .hero-main, .hero-side, .card {
      background: linear-gradient(160deg, rgba(26, 24, 21, 0.92), rgba(14, 13, 12, 0.85));
      border: 1px solid rgba(255, 244, 230, 0.16);
      border-radius: 20px;
      box-shadow: var(--shadow-soft);
      backdrop-filter: blur(18px) saturate(120%);
      min-width: 0;
      transition: box-shadow 160ms ease, border-color 160ms ease, transform 160ms ease;
    }
    .hero-main:hover, .hero-side:hover, .card:hover {
      border-color: rgba(126, 231, 214, 0.22);
      box-shadow: var(--shadow), var(--glow);
      transform: translateY(-1px);
    }
    .hero-main {
      padding: 20px 22px;
      overflow: hidden;
      position: relative;
    }
    .hero-head {
      display: flex;
      align-items: flex-start;
      justify-content: space-between;
      gap: 16px;
    }
    .hero-main::after {
      content: "";
      position: absolute;
      inset: auto -80px -80px auto;
      width: 220px;
      height: 220px;
      border-radius: 999px;
      background: radial-gradient(circle, rgba(121,230,215,0.22), rgba(121,230,215,0));
      pointer-events: none;
    }
    .eyebrow {
      display: inline-flex;
      padding: 7px 11px;
      border-radius: 999px;
      background: rgba(121,230,215,0.16);
      color: var(--accent-strong);
      border: 1px solid rgba(121, 230, 215, 0.26);
      font-size: 11px;
      font-weight: 700;
      letter-spacing: 0.12em;
      text-transform: uppercase;
    }
    h1 {
      margin: 10px 0 0;
      font-size: clamp(30px, 4.6vw, 52px);
      line-height: 0.96;
      letter-spacing: -0.05em;
    }
    .sub {
      color: var(--muted);
      margin-top: 12px;
      max-width: 720px;
      font-size: 14px;
      line-height: 1.55;
    }
    .hero-meta {
      display: flex;
      flex-wrap: wrap;
      gap: 8px;
      margin-top: 18px;
    }
    .pill {
      display: inline-flex;
      align-items: center;
      gap: 8px;
      border: 1px solid var(--line);
      border-radius: 999px;
      padding: 8px 12px;
      background: rgba(18, 16, 14, 0.9);
      color: var(--muted);
      box-shadow: inset 0 0 0 1px rgba(255, 255, 255, 0.02);
      font-size: 12px;
    }
    .hero-side {
      padding: 16px;
      display: grid;
      grid-template-columns: repeat(2, 1fr);
      gap: 10px;
    }
    .hero-stat {
      border-radius: 16px;
      border: 1px solid var(--line);
      background: linear-gradient(160deg, rgba(255,255,255,0.06), rgba(255,255,255,0.02));
      padding: 12px;
    }
    .hero-stat .k {
      color: var(--muted);
      font-size: 12px;
      letter-spacing: 0.08em;
      text-transform: uppercase;
    }
    .hero-stat .v {
      margin-top: 6px;
      font-size: 24px;
      font-weight: 800;
      letter-spacing: -0.04em;
    }
    .layout {
      display: grid;
      grid-template-columns: repeat(12, 1fr);
      gap: clamp(12px, 1.4vw, 18px);
      margin-top: clamp(12px, 1.4vw, 18px);
      align-items: stretch;
    }
    .card {
      grid-column: span 12;
      padding: 14px;
      overflow: hidden;
    }
    .span-8 { grid-column: span 8; }
    .span-6 { grid-column: span 6; }
    .span-4 { grid-column: span 4; }
    .metrics {
      display: grid;
      grid-template-columns: repeat(4, minmax(0, 1fr));
      gap: 10px;
    }
    .metrics.two {
      grid-template-columns: repeat(2, 1fr);
    }
    .metric {
      background: linear-gradient(160deg, rgba(255,255,255,0.06), rgba(255,255,255,0.02));
      border: 1px solid var(--line);
      border-radius: 16px;
      padding: 12px;
      box-shadow: inset 0 0 0 1px rgba(255, 255, 255, 0.02);
    }
    .metric .k {
      color: var(--muted);
      font-size: 12px;
      text-transform: uppercase;
      letter-spacing: 0.08em;
    }
    .metric .v {
      font-size: 24px;
      margin-top: 6px;
      font-weight: 800;
      letter-spacing: -0.04em;
    }
    .metric .small {
      margin-top: 6px;
      font-size: 12px;
    }
    .section-head {
      display: flex;
      justify-content: space-between;
      align-items: end;
      gap: 10px;
      flex-wrap: wrap;
      margin-bottom: 12px;
    }
    .title {
      font-size: 17px;
      font-weight: 800;
      letter-spacing: -0.03em;
    }
    .caption {
      color: var(--muted);
      font-size: 12px;
    }
    .table-shell {
      width: 100%;
      overflow-x: auto;
      overflow-y: hidden;
      border: 1px solid var(--line);
      border-radius: 16px;
      background: linear-gradient(180deg, rgba(255,255,255,0.04), rgba(255,255,255,0.02));
    }
    table {
      width: 100%;
      min-width: 100%;
      border-collapse: collapse;
      font-size: 13px;
    }
    th, td {
      padding: 8px 8px;
      border-bottom: 1px solid var(--line);
      text-align: left;
      vertical-align: top;
      overflow-wrap: anywhere;
      word-break: break-word;
    }
    th {
      color: var(--muted);
      font-weight: 700;
      font-size: 12px;
      text-transform: uppercase;
      letter-spacing: 0.08em;
      position: sticky;
      top: 0;
      z-index: 1;
      background: rgba(15, 14, 12, 0.96);
      backdrop-filter: blur(10px);
    }
    tbody tr:hover {
      background: rgba(121, 230, 215, 0.06);
    }
    .status {
      display: inline-flex;
      align-items: center;
      gap: 8px;
      padding: 6px 10px;
      border-radius: 999px;
      font-size: 12px;
      background: var(--ok-bg);
      color: var(--accent);
      font-weight: 700;
    }
    .status.bad {
      background: var(--danger-bg);
      color: var(--danger);
    }
    .small { color: var(--muted); font-size: 12px; }
    .mono { font-variant-numeric: tabular-nums; }
    .is-hidden { display: none; }
    .page-settings #overviewShell { display: none; }
    .page-settings #runtimeConfig { display: block; }
    a { color: var(--accent); text-decoration: none; }
    a:hover { text-decoration: underline; }
    .table-requests table { min-width: 1120px; }
    .table-models table { min-width: 720px; }
    .table-health table { min-width: 900px; }
    .table-usage table { min-width: 520px; }
    .table-cache table { min-width: 580px; }
    .error-feed {
      display: grid;
      gap: 12px;
    }
    .error-item {
      border: 1px solid var(--line);
      border-radius: 18px;
      background: linear-gradient(160deg, rgba(255,255,255,0.06), rgba(255,255,255,0.02));
      padding: 14px;
    }
    .error-top {
      display: flex;
      justify-content: space-between;
      align-items: flex-start;
      gap: 10px;
      flex-wrap: wrap;
    }
    .error-heading {
      display: flex;
      align-items: baseline;
      flex-wrap: wrap;
      gap: 8px;
    }
    .error-title {
      font-size: 16px;
      font-weight: 800;
      letter-spacing: -0.02em;
    }
    .error-message {
      margin-top: 10px;
      line-height: 1.55;
      overflow-wrap: anywhere;
      word-break: break-word;
    }
    .error-meta {
      display: flex;
      flex-wrap: wrap;
      gap: 8px;
      margin-top: 12px;
    }
    .tag {
      display: inline-flex;
      align-items: center;
      padding: 6px 10px;
      border-radius: 999px;
      border: 1px solid var(--line);
      background: rgba(255,255,255,0.06);
      color: var(--muted);
      font-size: 12px;
      line-height: 1.2;
    }
    .tag.accent {
      color: var(--accent);
      border-color: rgba(121, 230, 215, 0.24);
      background: rgba(121, 230, 215, 0.08);
    }
    .config-panel {
      display: grid;
      gap: 16px;
    }
    .settings-summary {
      display: grid;
      grid-template-columns: repeat(4, minmax(0, 1fr));
      gap: 8px;
    }
    .settings-jumpbar {
      display: flex;
      flex-wrap: wrap;
      gap: 6px;
      margin-bottom: 4px;
    }
    .settings-jumpbar a {
      display: inline-flex;
      align-items: center;
      padding: 6px 10px;
      border-radius: 999px;
      border: 1px solid var(--line);
      background: rgba(255,255,255,0.04);
      color: var(--muted);
      font-size: 11px;
      text-decoration: none;
    }
    .settings-jumpbar a:hover {
      color: var(--ink);
      border-color: rgba(126, 231, 214, 0.4);
      text-decoration: none;
    }
    .config-grid {
      display: grid;
      grid-template-columns: repeat(2, minmax(0, 1fr));
      gap: 10px;
    }
    .config-field {
      display: flex;
      flex-direction: column;
      gap: 5px;
    }
    .config-field label {
      font-size: 11px;
      letter-spacing: 0.08em;
      text-transform: uppercase;
      color: var(--muted);
    }
    .config-field input,
    .config-field textarea,
    .config-field select {
      border-radius: 12px;
      border: 1px solid var(--line);
      background: rgba(255,255,255,0.06);
      padding: 9px 11px;
      font-size: 12px;
      color: var(--ink);
      outline: none;
      transition: border-color 120ms ease, box-shadow 120ms ease, background 120ms ease;
    }
    .config-field input:focus,
    .config-field textarea:focus,
    .config-field select:focus,
    .config-search:focus {
      border-color: rgba(126, 231, 214, 0.5);
      box-shadow: 0 0 0 3px rgba(126, 231, 214, 0.12);
      background: rgba(10, 10, 9, 0.55);
    }
    .config-field textarea {
      min-height: 82px;
      resize: vertical;
    }
    .config-actions {
      display: flex;
      flex-wrap: wrap;
      gap: 8px;
      align-items: center;
    }
    .btn {
      border-radius: 999px;
      border: 1px solid rgba(121, 230, 215, 0.4);
      background: linear-gradient(120deg, rgba(121, 230, 215, 0.16), rgba(121, 230, 215, 0.08));
      color: var(--accent);
      padding: 7px 13px;
      font-size: 11px;
      font-weight: 700;
      letter-spacing: 0.08em;
      text-transform: uppercase;
      cursor: pointer;
      box-shadow: inset 0 0 0 1px rgba(255,255,255,0.04);
      transition: transform 120ms ease, box-shadow 120ms ease, border-color 120ms ease;
    }
    .btn:hover {
      border-color: rgba(126, 231, 214, 0.65);
      box-shadow: 0 12px 26px rgba(121, 230, 215, 0.16);
      transform: translateY(-1px);
    }
    .btn:active {
      transform: translateY(0);
    }
    .btn.secondary {
      border-color: var(--line);
      color: var(--muted);
      background: rgba(255,255,255,0.06);
    }
    .btn.danger {
      border-color: rgba(255, 127, 110, 0.5);
      color: var(--danger);
      background: rgba(255, 127, 110, 0.12);
    }
    .btn.link {
      display: inline-flex;
      align-items: center;
      justify-content: center;
      text-decoration: none;
    }
    .config-card {
      border: 1px solid var(--line);
      border-radius: 16px;
      padding: 10px;
      background: linear-gradient(160deg, rgba(255,255,255,0.06), rgba(255,255,255,0.02));
      display: grid;
      gap: 8px;
    }
    .config-card-head {
      display: flex;
      justify-content: space-between;
      align-items: flex-start;
      gap: 6px;
    }
    .config-card-title {
      font-weight: 700;
    }
    .config-card-head-main {
      display: grid;
      gap: 4px;
      min-width: 0;
    }
    .config-help {
      color: var(--muted);
      font-size: 11px;
    }
    .config-status {
      color: var(--muted);
      font-size: 11px;
    }
    .config-toolbar {
      display: flex;
      flex-wrap: wrap;
      gap: 8px;
      align-items: center;
      margin-bottom: 4px;
    }
    .config-search {
      min-width: min(360px, 100%);
      flex: 1 1 240px;
      border-radius: 999px;
      border: 1px solid var(--line);
      background: rgba(255,255,255,0.06);
      padding: 9px 12px;
      font-size: 12px;
      color: var(--ink);
      outline: none;
      transition: border-color 120ms ease, box-shadow 120ms ease, background 120ms ease;
    }
    .config-card.collapsed > :not(.config-card-head) {
      display: none;
    }
    .config-card.hidden-search {
      display: none;
    }
    .validation-summary {
      display: none;
      gap: 6px;
      padding: 10px 12px;
      border-radius: 14px;
      border: 1px solid rgba(255, 127, 110, 0.35);
      background: rgba(255, 127, 110, 0.10);
      color: var(--ink);
      font-size: 12px;
      line-height: 1.5;
    }
    .validation-summary.visible {
      display: grid;
    }
    .validation-list {
      margin: 0;
      padding-left: 18px;
    }
    .input-invalid {
      border-color: rgba(255, 127, 110, 0.75) !important;
      box-shadow: 0 0 0 3px rgba(255, 127, 110, 0.10);
    }
    .validation-warning {
      border-color: rgba(240, 179, 90, 0.55) !important;
      box-shadow: 0 0 0 3px rgba(240, 179, 90, 0.08);
    }
    .field-message {
      margin-top: 4px;
      font-size: 11px;
      line-height: 1.4;
    }
    .field-message.error {
      color: var(--danger);
    }
    .field-message.warning {
      color: var(--amber);
    }
    .history-list {
      display: grid;
      gap: 10px;
    }
    .probe-status {
      display: grid;
      gap: 3px;
      width: 100%;
      padding: 8px 10px;
      border-radius: 12px;
      border: 1px solid var(--line);
      background: rgba(0,0,0,0.18);
      font-size: 11px;
    }
    .probe-status.ok {
      border-color: rgba(121, 230, 215, 0.35);
      background: rgba(121, 230, 215, 0.08);
    }
    .probe-status.fail {
      border-color: rgba(255, 127, 110, 0.4);
      background: rgba(255, 127, 110, 0.08);
    }
    .probe-summary {
      display: flex;
      flex-wrap: wrap;
      gap: 6px;
      align-items: center;
    }
    .probe-preview {
      color: var(--muted);
      overflow-wrap: anywhere;
      word-break: break-word;
      white-space: pre-wrap;
      max-height: 120px;
      overflow: auto;
    }
    .history-item {
      display: flex;
      justify-content: space-between;
      align-items: center;
      gap: 10px;
      padding: 10px;
      border-radius: 14px;
      border: 1px solid var(--line);
      background: linear-gradient(160deg, rgba(255,255,255,0.06), rgba(255,255,255,0.02));
    }
    .config-footer {
      position: sticky;
      bottom: 0;
      z-index: 2;
      padding: 10px 0 0;
      background: linear-gradient(180deg, rgba(7,7,6,0), rgba(7,7,6,0.9) 28%, rgba(7,7,6,0.98));
    }
    .config-footer .config-actions {
      padding: 10px 12px;
      border-radius: 14px;
      border: 1px solid var(--line);
      background: rgba(14, 13, 12, 0.92);
      box-shadow: var(--shadow-soft);
    }
    .config-hint {
      display: inline-flex;
      align-items: center;
      min-height: 30px;
      padding: 0 2px;
      font-size: 11px;
      color: var(--muted);
    }
    .history-meta {
      display: grid;
      gap: 4px;
      min-width: 0;
    }
    .history-name {
      font-weight: 700;
      overflow-wrap: anywhere;
      word-break: break-word;
    }
    .card-fill {
      display: flex;
      flex-direction: column;
      height: clamp(420px, 54vh, 760px);
      min-height: 0;
    }
    .card-fill-body {
      flex: 1 1 auto;
      min-height: 0;
      overflow: auto;
    }
    .chart-wrap {
      position: relative;
      width: 100%;
      height: 220px;
      overflow: hidden;
    }
    .chart-wrap svg {
      display: block;
      width: 100%;
      height: 100%;
    }
    .chart-tooltip {
      position: absolute;
      pointer-events: none;
      padding: 8px 12px;
      border-radius: 10px;
      background: var(--panel-strong);
      border: 1px solid var(--line);
      color: var(--ink);
      font-size: 12px;
      line-height: 1.5;
      white-space: nowrap;
      box-shadow: var(--shadow-soft);
      opacity: 0;
      transition: opacity 100ms ease;
      z-index: 10;
    }
    .chart-tooltip.visible {
      opacity: 1;
    }
    .chart-legend {
      display: flex;
      flex-wrap: wrap;
      gap: 12px;
      margin-top: 10px;
    }
    .chart-legend-item {
      display: inline-flex;
      align-items: center;
      gap: 6px;
      font-size: 12px;
      color: var(--muted);
    }
    .chart-legend-dot {
      width: 10px;
      height: 10px;
      border-radius: 3px;
    }
    .chart-controls {
      display: flex;
      gap: 6px;
      flex-wrap: wrap;
    }
    .chart-controls button {
      border-radius: 999px;
      border: 1px solid var(--line);
      background: rgba(255,255,255,0.04);
      color: var(--muted);
      padding: 5px 12px;
      font-size: 11px;
      font-weight: 600;
      cursor: pointer;
      transition: border-color 120ms ease, color 120ms ease, background 120ms ease;
    }
    .chart-controls button:hover {
      border-color: rgba(126, 231, 214, 0.4);
      color: var(--ink);
    }
    .chart-controls button.active {
      border-color: rgba(126, 231, 214, 0.5);
      background: rgba(126, 231, 214, 0.12);
      color: var(--accent);
    }
    .diff-summary {
      display: flex;
      flex-wrap: wrap;
      gap: 8px;
      margin-bottom: 10px;
    }
    .diff-shell {
      border: 1px solid var(--line);
      border-radius: 14px;
      background: rgba(0,0,0,0.3);
      overflow: hidden;
    }
    .diff-lines {
      margin: 0;
      padding: 12px 0;
      max-height: 340px;
      overflow: auto;
      font-size: 12px;
      line-height: 1.5;
    }
    .diff-line {
      display: grid;
      grid-template-columns: 18px 1fr;
      gap: 10px;
      padding: 0 12px;
      white-space: pre-wrap;
      word-break: break-word;
      font-family: "Cascadia Code", "Consolas", monospace;
    }
    .diff-line + .diff-line {
      margin-top: 2px;
    }
    .diff-line.add {
      background: rgba(121, 230, 215, 0.10);
    }
    .diff-line.remove {
      background: rgba(255, 127, 110, 0.12);
    }
    .diff-line.context {
      color: var(--muted);
    }
    .diff-mark {
      text-align: center;
      font-weight: 700;
    }
    .table-shell::-webkit-scrollbar {
      height: 10px;
    }
    .table-shell::-webkit-scrollbar-thumb {
      background: rgba(121, 230, 215, 0.28);
      border-radius: 999px;
    }
    @media (max-width: 900px) {
      .hero {
        grid-template-columns: 1fr;
      }
      .span-8, .span-6, .span-4 {
        grid-column: span 12;
      }
      .metrics, .hero-side, .settings-summary {
        grid-template-columns: repeat(2, 1fr);
      }
    }
    @media (max-width: 640px) {
      .metrics, .hero-side, .settings-summary {
        grid-template-columns: 1fr;
      }
    }
    @media (max-width: 1180px) {
      .span-4, .span-8 {
        grid-column: span 12;
      }
    }
    @media (max-width: 920px) {
      .topbar, .hero-head {
        flex-direction: column;
        align-items: stretch;
      }
      .topnav {
        justify-content: flex-start;
      }
    }
    @media (min-width: 1600px) {
      .hero {
        grid-template-columns: 1.45fr 0.55fr;
      }
      h1 {
        font-size: clamp(40px, 4vw, 72px);
      }
      .sub {
        max-width: 880px;
      }
    }
  </style>
</head>
<body class="{{BODY_CLASS}}">
  <div class="wrap">
    <div class="topbar">
      <div class="brand">
        <div class="brand-mark" aria-hidden="true">
          <svg viewBox="0 0 96 96" fill="none" xmlns="http://www.w3.org/2000/svg">
            <path d="M24 68V28h14l10 24 10-24h14v40H62V46L54 66H42l-8-20v22H24Z" fill="#7EE7D6"/>
            <circle cx="73" cy="24" r="8" fill="#F1B866"/>
          </svg>
        </div>
        <div class="brand-copy">
          <div class="brand-title">AI Model Gateway</div>
          <div class="brand-subtitle">Unified observability, routing, pricing, and runtime control.</div>
        </div>
      </div>
      <div class="topnav">
        <a href="#performance">Performance</a>
        <a href="#economics">Economics</a>
        <a href="#upstreams-card">Upstreams</a>
        <a href="#requests-card">Requests</a>
        <a href="{{SETTINGS_HREF}}">{{SETTINGS_LABEL}}</a>
      </div>
    </div>
    <div class="hero">
      <div class="hero-main">
        <div class="hero-head">
          <div>
            <div class="eyebrow">AI Gateway Admin</div>
            <h1>Ops, Cost, Throughput.</h1>
          </div>
        </div>
        <div class="sub">把请求量、吞吐、延迟、失败轨迹和 USD 成本放在同一块面板里，先判断是不是上游波动，再判断是不是代理放大。</div>
        <div class="hero-meta">
          <div class="pill" id="generatedAt">加载中</div>
          <div class="pill" id="pricingSource">Pricing source</div>
          <div class="pill" id="bridgeState">Bridge</div>
          <a class="btn secondary link" href="{{SETTINGS_HREF}}" id="openSettings">{{SETTINGS_LABEL}}</a>
        </div>
      </div>
      <div class="hero-side" id="heroStats"></div>
    </div>

    <div id="overviewShell">
    <div class="card" id="performance">
      <div class="section-head">
        <div>
          <div class="title">Live Performance</div>
          <div class="caption">最近 1 分钟与 5 分钟窗口内的真实 RPM / TPM / 延迟。</div>
        </div>
      </div>
      <div class="metrics" id="metrics"></div>
    </div>

    <div class="layout" id="chartLayout">
      <div class="card span-6">
        <div class="section-head">
          <div>
            <div class="title">Request Throughput</div>
            <div class="caption">RPM 与 TPM 趋势，按时间桶聚合。</div>
          </div>
          <div class="chart-controls" id="chartRangeControls">
            <button data-hours="1" data-bucket="5">1h</button>
            <button data-hours="6" data-bucket="15">6h</button>
            <button data-hours="24" data-bucket="60" class="active">24h</button>
            <button data-hours="72" data-bucket="180">3d</button>
            <button data-hours="168" data-bucket="360">7d</button>
          </div>
        </div>
        <div class="chart-wrap" id="chartRpm"><div class="chart-tooltip" id="tipRpm"></div></div>
        <div class="chart-legend" id="legendRpm"></div>
      </div>
      <div class="card span-6">
        <div class="section-head">
          <div>
            <div class="title">Latency Trend</div>
            <div class="caption">平均延迟（ms）与成功率趋势。</div>
          </div>
        </div>
        <div class="chart-wrap" id="chartLatency"><div class="chart-tooltip" id="tipLatency"></div></div>
        <div class="chart-legend" id="legendLatency"></div>
      </div>
      <div class="card span-6">
        <div class="section-head">
          <div>
            <div class="title">Token Usage by Upstream</div>
            <div class="caption">按上游分组的 token 消耗堆叠柱状图。</div>
          </div>
        </div>
        <div class="chart-wrap" id="chartTokens"><div class="chart-tooltip" id="tipTokens"></div></div>
        <div class="chart-legend" id="legendTokens"></div>
      </div>
      <div class="card span-6">
        <div class="section-head">
          <div>
            <div class="title">Success / Failure</div>
            <div class="caption">每个时间桶内的成功与失败请求数。</div>
          </div>
        </div>
        <div class="chart-wrap" id="chartSuccess"><div class="chart-tooltip" id="tipSuccess"></div></div>
        <div class="chart-legend" id="legendSuccess"></div>
      </div>
    </div>

    <div class="layout">
      <div class="card span-8" id="economics">
        <div class="section-head">
          <div>
            <div class="title">Model Economics</div>
            <div class="caption">按模型汇总 token、估算美元成本和官方价格表覆盖情况。</div>
          </div>
        </div>
        <div id="byModel"></div>
      </div>
      <div class="card span-4">
        <div class="section-head">
          <div>
            <div class="title">Cost Snapshot</div>
            <div class="caption">基于已知官方价格模型估算，总额单位为美元。</div>
          </div>
        </div>
        <div class="metrics" id="costMetrics"></div>
      </div>
      <div class="card span-6" id="upstreams-card">
        <div class="section-head">
          <div>
            <div class="title">Upstream Health</div>
            <div class="caption">看当前探活状态、重试性失败计数和冷却信息。</div>
          </div>
        </div>
        <div id="upstreams"></div>
      </div>
      <div class="card span-6">
        <div class="section-head">
          <div>
            <div class="title">Upstream Usage</div>
            <div class="caption">按上游汇总 token 消耗，方便和健康状态对照。</div>
          </div>
        </div>
        <div id="byUpstream"></div>
      </div>
      <div class="card span-4">
        <div class="section-head">
          <div>
            <div class="title">Cache Trends</div>
            <div class="caption">最近 1 小时 / 24 小时的缓存命中率。</div>
          </div>
        </div>
        <div class="metrics two" id="cacheTrends"></div>
      </div>
      <div class="card span-8">
        <div class="section-head">
          <div>
            <div class="title">Cache Hit Ranking</div>
            <div class="caption">最近 24 小时按上游缓存命中率排序。</div>
          </div>
        </div>
        <div id="cacheRanking"></div>
      </div>
      <div class="card span-4 card-fill">
        <div class="section-head">
          <div>
            <div class="title">Recent Errors</div>
            <div class="caption">最近错误优先看消息类型和上游归属。</div>
          </div>
        </div>
        <div class="card-fill-body" id="errors"></div>
      </div>
      <div class="card span-8 card-fill" id="requests-card">
        <div class="section-head">
          <div>
            <div class="title">Recent Requests</div>
            <div class="caption">最新请求轨迹，含状态、尝试次数、延迟和单次估算成本。</div>
          </div>
        </div>
        <div class="card-fill-body" id="requests"></div>
      </div>
    </div>
    <div class="card span-12 is-hidden" id="runtimeConfig">
        <div class="section-head">
          <div>
            <div class="title">Runtime Config</div>
            <div class="caption">编辑 health、bridge、重试、拦截和上游服务商配置，支持导出当前配置与回滚上一个版本。</div>
          </div>
          <div class="config-status" id="configStatus">加载中</div>
        </div>
        <div class="config-panel">
          <div class="settings-summary">
            <div class="metric"><div class="k">Surface</div><div class="v mono">Config</div><div class="small">Runtime edit without YAML</div></div>
            <div class="metric"><div class="k">Providers</div><div class="v mono" id="settingsProviderCount">0</div><div class="small">Active upstream entries</div></div>
            <div class="metric"><div class="k">Health Path</div><div class="v mono" id="settingsHealthPath">/v1/models</div><div class="small">Probe target</div></div>
            <div class="metric"><div class="k">Mode</div><div class="v mono">Safe Edit</div><div class="small">Save, export, diff, rollback</div></div>
          </div>
          <div class="config-toolbar">
            <input class="config-search" id="configSearch" type="search" placeholder="Search config sections, fields, providers..." />
            <button class="btn secondary" id="expandSections" type="button">Expand All</button>
            <button class="btn secondary" id="collapseSections" type="button">Collapse All</button>
          </div>
          <div class="settings-jumpbar">
            <a href="#cfg-health">Health</a>
            <a href="#cfg-bridge">Bridge</a>
            <a href="#cfg-router">Retry</a>
            <a href="#cfg-intercepts">Intercepts</a>
            <a href="#cfg-upstreams">Providers</a>
            <a href="#cfg-history">History</a>
          </div>
          <div class="validation-summary" id="configValidation"></div>
          <div class="config-card config-section" id="cfg-health" data-section-title="Health Check">
            <div class="config-card-head">
              <div class="config-card-head-main">
                <div class="config-card-title">Health Check</div>
                <div class="config-help">控制主动探活的开关、间隔、超时和路径</div>
              </div>
            </div>
            <div class="config-grid">
              <div class="config-field">
                <label>Enabled</label>
                <label class="small"><input type="checkbox" id="cfgHealthEnabled"> Health checks enabled</label>
              </div>
              <div class="config-field">
                <label>Path</label>
                <input type="text" id="cfgHealthPath" placeholder="/v1/models" />
              </div>
              <div class="config-field">
                <label>Interval (sec)</label>
                <input type="number" min="0" id="cfgHealthInterval" />
              </div>
              <div class="config-field">
                <label>Timeout (ms)</label>
                <input type="number" min="0" id="cfgHealthTimeout" />
              </div>
            </div>
          </div>
          <div class="config-card config-section" id="cfg-bridge" data-section-title="Model Bridge">
            <div class="config-card-head">
              <div class="config-card-head-main">
                <div class="config-card-title">Model Bridge</div>
                <div class="config-help">维护模型别名映射和需跳过桥接的 User-Agent</div>
              </div>
            </div>
            <div class="config-grid">
              <div class="config-field">
                <label>Enabled</label>
                <label class="small"><input type="checkbox" id="cfgBridgeEnabled"> Rewrite requested model before routing</label>
              </div>
              <div class="config-field">
                <label>Exclude User-Agents</label>
                <textarea id="cfgBridgeExcludeUA" placeholder="OpenAI-Python/*&#10;curl/*"></textarea>
              </div>
            </div>
            <div id="bridgeRuleList"></div>
            <div class="config-actions">
              <button class="btn secondary" id="addBridgeRule">Add Bridge Rule</button>
            </div>
          </div>
          <div class="config-card config-section" id="cfg-router" data-section-title="Router Retry">
            <div class="config-card-head">
              <div class="config-card-head-main">
                <div class="config-card-title">Router Retry</div>
                <div class="config-help">控制重试次数与退避窗口</div>
              </div>
            </div>
            <div class="config-grid">
              <div class="config-field">
                <label>Max Retries</label>
                <input type="number" min="0" id="cfgMaxRetries" />
              </div>
              <div class="config-field">
                <label>Backoff Base (ms)</label>
                <input type="number" min="0" id="cfgBackoff" />
              </div>
              <div class="config-field">
                <label>Backoff Max (ms)</label>
                <input type="number" min="0" id="cfgBackoffMax" />
              </div>
              <div class="config-field">
                <label>Failure Threshold</label>
                <input type="number" min="0" id="cfgFailureThreshold" />
              </div>
              <div class="config-field">
                <label>Cooldown (sec)</label>
                <input type="number" min="0" id="cfgCooldown" />
              </div>
              <div class="config-field">
                <label>Passthrough After (sec)</label>
                <input type="number" min="0" id="cfgPassthrough" />
              </div>
            </div>
          </div>
          <div class="config-card config-section" data-section-title="Retry Policy">
            <div class="config-card-head">
              <div class="config-card-head-main">
                <div class="config-card-title">Retry Policy</div>
                <div class="config-help">命中状态码或关键字后触发重试</div>
              </div>
            </div>
            <div class="config-grid">
              <div class="config-field">
                <label>Status Codes</label>
                <input type="text" id="cfgRetryCodes" placeholder="408,429,500,502,503,504" />
              </div>
              <div class="config-field">
                <label>Status Code Min</label>
                <input type="number" min="0" id="cfgRetryMin" placeholder="500" />
              </div>
              <div class="config-field" style="grid-column: span 2;">
                <label>Message Keywords</label>
                <textarea id="cfgRetryKeywords" placeholder="rate limit\nupstream request failed"></textarea>
              </div>
            </div>
          </div>
          <div class="config-card config-section" id="cfg-intercepts" data-section-title="Response Intercepts">
            <div class="config-card-head">
              <div class="config-card-head-main">
                <div class="config-card-title">Response Intercepts</div>
                <div class="config-help">按路径、状态码或错误关键字提前判定 retry / fail</div>
              </div>
            </div>
            <div id="interceptList"></div>
            <div class="config-actions">
              <button class="btn secondary" id="addIntercept">Add Intercept</button>
            </div>
          </div>
          <div class="config-card config-section" id="cfg-upstreams" data-section-title="Service Providers">
            <div class="config-card-head">
              <div class="config-card-head-main">
                <div class="config-card-title">Service Providers</div>
                <div class="config-help">维护上游服务商的 URL、API key、模型范围和超时；每一项都可先行测试再保存</div>
              </div>
            </div>
            <div id="upstreamConfigList"></div>
            <div class="config-actions">
              <button class="btn secondary" id="addUpstream">Add Provider</button>
            </div>
          </div>
          <div class="config-card config-section" id="cfg-history" data-section-title="Config History">
            <div class="config-card-head">
              <div class="config-card-head-main">
                <div class="config-card-title">Config History</div>
                <div class="config-help">保存配置前会自动归档旧版本，可选择具体版本回滚</div>
              </div>
            </div>
            <div id="configHistoryList"></div>
            <div id="configDiffPreview"></div>
          </div>
          <div class="config-footer">
          <div class="config-actions">
            <button class="btn" id="saveConfig">Save Config</button>
            <button class="btn secondary" id="reloadConfig">Reload</button>
            <button class="btn secondary" id="exportConfig">Export</button>
            <button class="btn danger" id="rollbackConfig">Rollback</button>
            <span class="config-hint" id="configHint"></span>
          </div>
          </div>
        </div>
      </div>
    </div>
  </div>
  <script>
    const fmt = new Intl.NumberFormat("zh-CN");
    const fmtUsd = new Intl.NumberFormat("en-US", { style: "currency", currency: "USD", minimumFractionDigits: 4, maximumFractionDigits: 4 });
    const relativeTime = (ts) => {
      if (!ts) return "-";
      const delta = Math.max(0, Date.now() - new Date(ts).getTime());
      if (delta < 60000) return Math.round(delta / 1000) + "s 前";
      if (delta < 3600000) return Math.round(delta / 60000) + "m 前";
      return Math.round(delta / 3600000) + "h 前";
    };
    const fmtRate = (value) => Number(value || 0).toFixed(1);
    const fmtMs = (value) => fmt.format(Math.round(value || 0)) + " ms";
    const fmtPct = (value) => Number(value || 0).toFixed(1) + "%";
    const fmtMoney = (value) => fmtUsd.format(Number(value || 0));
    const fmtBytes = (value) => {
      const size = Number(value || 0);
      if (size < 1024) return size + ' B';
      if (size < 1024 * 1024) return (size / 1024).toFixed(1) + ' KB';
      return (size / (1024 * 1024)).toFixed(1) + ' MB';
    };
    const statusPill = (healthy) => healthy ? '<span class="status">healthy</span>' : '<span class="status bad">degraded</span>';
    const escapeHTML = (value) => String(value ?? '-')
      .replaceAll('&', '&amp;')
      .replaceAll('<', '&lt;')
      .replaceAll('>', '&gt;')
      .replaceAll('"', '&quot;')
      .replaceAll("'", '&#39;');
    const priceLine = (pricing) => {
      if (!pricing) return '<span class="small">official price unavailable</span>';
      return '<span class="small mono">$' + pricing.input_per_1m_usd + ' / $' + pricing.output_per_1m_usd + ' per 1M</span>';
    };
    const resolveRequestPrice = (item, pricing) => {
      const routeCatalog = pricing.route_catalog || {};
      const catalog = pricing.catalog || {};
      const requested = (item.requested_model || '').trim().toLowerCase();
      const effective = (item.model || '').trim().toLowerCase();
      const routeKey = requested + '|' + effective;
      return routeCatalog[routeKey] || catalog[requested] || catalog[effective] || null;
    };
    const estimateRequestCost = (item, pricing) => {
      const price = resolveRequestPrice(item, pricing);
      if (!price || !item.usage) return '';
      const cachedPromptTokens = Math.max(0, Math.min(item.usage.cached_prompt_tokens || 0, item.usage.prompt_tokens || 0));
      const uncachedPromptTokens = Math.max(0, (item.usage.prompt_tokens || 0) - cachedPromptTokens);
      let promptUsd = (uncachedPromptTokens / 1000000) * (price.input_per_1m_usd || 0);
      if (cachedPromptTokens > 0) {
        const cachedRate = price.cached_input_per_1m_usd || price.input_per_1m_usd || 0;
        promptUsd += (cachedPromptTokens / 1000000) * cachedRate;
      }
      const completionUsd = ((item.usage.completion_tokens || 0) / 1000000) * (price.output_per_1m_usd || 0);
      return fmtMoney(promptUsd + completionUsd);
    };
    const promptUsageCell = (usage) => {
      const promptTokens = fmt.format((usage && usage.prompt_tokens) || 0);
      const cachedTokens = (usage && usage.cached_prompt_tokens) || 0;
      if (!cachedTokens) return promptTokens;
      return promptTokens + '<br><span class="small">cached ' + fmt.format(cachedTokens) + '</span>';
    };
    const totalUsageCell = (usage) => {
      const totalTokens = fmt.format((usage && usage.total_tokens) || 0);
      const cachedTokens = (usage && usage.cached_prompt_tokens) || 0;
      if (!cachedTokens) return totalTokens;
      return totalTokens + '<br><span class="small">prompt cache ' + fmt.format(cachedTokens) + '</span>';
    };
    const cacheHitRate = (usage) => {
      const promptTokens = Math.max(0, (usage && usage.prompt_tokens) || 0);
      const cachedTokens = Math.max(0, Math.min((usage && usage.cached_prompt_tokens) || 0, promptTokens));
      if (!promptTokens) return null;
      return (cachedTokens / promptTokens) * 100;
    };
    const cacheRateCell = (usage) => {
      const rate = cacheHitRate(usage);
      if (rate === null) return '<span class="small">n/a</span>';
      const cachedTokens = Math.max(0, (usage && usage.cached_prompt_tokens) || 0);
      return fmtPct(rate) + '<br><span class="small">' + fmt.format(cachedTokens) + ' cached</span>';
    };
    const cacheTrendDetail = (window) => {
      const cached = fmt.format((window && window.cached_prompt_tokens) || 0);
      const prompt = fmt.format((window && window.prompt_tokens) || 0);
      const reqs = fmt.format((window && window.requests) || 0);
      return cached + ' / ' + prompt + ' prompt · ' + reqs + ' reqs';
    };
    const modelFlow = (item) => {
      const requested = escapeHTML(item.requested_model || item.model || '-');
      const effective = escapeHTML(item.model || '-');
      if (!item.requested_model || item.requested_model === item.model) {
        return effective;
      }
      return requested + ' <span class="small">-&gt;</span> ' + effective;
    };
    const table = (headers, rows, className = '') => {
      if (!rows.length) return '<div class="small">暂无数据</div>';
      return '<div class="table-shell ' + className + '"><table><thead><tr>' + headers.map(h => '<th>' + h + '</th>').join('') + '</tr></thead><tbody>' + rows.map(row => '<tr>' + row.map(cell => '<td>' + cell + '</td>').join('') + '</tr>').join('') + '</tbody></table></div>';
    };
    const errorFeed = (items) => {
      if (!items.length) return '<div class="small">暂无数据</div>';
      return '<div class="error-feed">' + items.slice(0, 16).map(item => {
        const code = Number(item.status_code || 0);
        const upstream = escapeHTML(item.upstream || '-');
        const model = escapeHTML(item.model || '-');
        const attempt = escapeHTML(item.attempt || '-');
        const message = escapeHTML(item.message || '-');
        const badge = code >= 400 ? '<span class="status bad">' + escapeHTML(code || '-') + '</span>' : '<span class="status">' + escapeHTML(code || '-') + '</span>';
        return '<article class="error-item">'
          + '<div class="error-top">'
          + '<div>'
          + '<div class="error-heading"><div class="error-title mono">' + escapeHTML(relativeTime(item.timestamp)) + '</div><div class="small">attempt ' + attempt + '</div></div>'
          + '<div class="error-message">' + message + '</div>'
          + '</div>'
          + badge
          + '</div>'
          + '<div class="error-meta">'
          + '<span class="tag">' + upstream + '</span>'
          + '<span class="tag">' + model + '</span>'
          + '<span class="tag accent">' + escapeHTML(item.route_mode || 'direct') + '</span>'
          + '<span class="tag">status ' + escapeHTML(code || '-') + '</span>'
          + '</div>'
          + '</article>';
      }).join('') + '</div>';
    };
    const byId = (id) => document.getElementById(id);
    const revealSettingsIfHash = () => {
      const panel = byId('runtimeConfig');
      if (!panel) return;
      if (document.body.classList.contains('page-settings') || window.location.hash === '#runtimeConfig') {
        panel.classList.remove('is-hidden');
      }
    };
    const topLevelSections = () => Array.from(document.querySelectorAll('.config-section'));
    const listToString = (items) => (items || []).join(", ");
    const keywordsToString = (items) => (items || []).join("\n");
    const headersToString = (headers) => Object.entries(headers || {})
      .map(([key, value]) => key + ': ' + value)
      .join("\n");
    const parseList = (value) => String(value || "")
      .split(/[\n,]+/)
      .map(v => v.trim())
      .filter(Boolean);
    const parseCodes = (value) => parseList(value)
      .map(v => Number.parseInt(v, 10))
      .filter(v => Number.isFinite(v));
    const readNumber = (el) => {
      const value = Number.parseInt(el.value, 10);
      return Number.isFinite(value) ? value : 0;
    };
    const readOptionalNumber = (el) => {
      const raw = String(el.value || "").trim();
      if (raw === "") return null;
      const value = Number.parseInt(raw, 10);
      return Number.isFinite(value) ? value : null;
    };
    const parseHeaders = (value) => {
      const lines = String(value || "")
        .split(/\n+/)
        .map(v => v.trim())
        .filter(Boolean);
      const headers = {};
      for (const line of lines) {
        const idx = line.indexOf(':');
        if (idx <= 0) continue;
        const key = line.slice(0, idx).trim();
        const headerValue = line.slice(idx + 1).trim();
        if (!key) continue;
        headers[key] = headerValue;
      }
      return headers;
    };
    const ensureSectionControls = () => {
      topLevelSections().forEach((section) => {
        const head = section.querySelector('.config-card-head');
        if (!head || head.querySelector('.section-toggle')) return;
        const actions = document.createElement('div');
        actions.className = 'config-actions';
        actions.innerHTML = '<button class="btn secondary section-toggle" type="button">Collapse</button>';
        head.appendChild(actions);
      });
    };
    const setSectionCollapsed = (section, collapsed) => {
      section.classList.toggle('collapsed', !!collapsed);
      const button = section.querySelector('.section-toggle');
      if (button) {
        button.textContent = collapsed ? 'Expand' : 'Collapse';
      }
    };
    const applyConfigSearch = () => {
      const query = String(byId('configSearch')?.value || '').trim().toLowerCase();
      topLevelSections().forEach((section) => {
        const title = String(section.getAttribute('data-section-title') || '').toLowerCase();
        const text = String(section.textContent || '').toLowerCase();
        const matched = !query || title.includes(query) || text.includes(query);
        section.classList.toggle('hidden-search', !matched);
      });
    };
    const clearValidationState = () => {
      document.querySelectorAll('.input-invalid').forEach((el) => el.classList.remove('input-invalid'));
      document.querySelectorAll('.validation-warning').forEach((el) => el.classList.remove('validation-warning'));
      document.querySelectorAll('.field-message').forEach((el) => el.remove());
      const host = byId('configValidation');
      if (host) {
        host.classList.remove('visible');
        host.innerHTML = '';
      }
    };
    const renderValidationSummary = (errors, warnings) => {
      const host = byId('configValidation');
      if (!host) return;
      if (!errors.length && !warnings.length) {
        host.classList.remove('visible');
        host.innerHTML = '';
        return;
      }
      let html = '';
      if (errors.length) {
        html += '<div><strong>Save blocked:</strong><ul class="validation-list">' + errors.map((msg) => '<li>' + escapeHTML(msg) + '</li>').join('') + '</ul></div>';
      }
      if (warnings.length) {
        html += '<div><strong>Warnings:</strong><ul class="validation-list">' + warnings.map((msg) => '<li>' + escapeHTML(msg) + '</li>').join('') + '</ul></div>';
      }
      host.innerHTML = html;
      host.classList.add('visible');
    };
    const setFieldMessage = (el, message, warning = false) => {
      if (!el) return;
      const field = el.closest('.config-field');
      if (!field) return;
      let msg = field.querySelector('.field-message');
      if (!msg) {
        msg = document.createElement('div');
        msg.className = 'field-message';
        field.appendChild(msg);
      }
      msg.className = 'field-message ' + (warning ? 'warning' : 'error');
      msg.textContent = message;
    };
    const markInvalid = (el, message = '', warning = false) => {
      if (!el) return;
      el.classList.add(warning ? 'validation-warning' : 'input-invalid');
      if (message) {
        setFieldMessage(el, message, warning);
      }
    };
    const isValidHTTPURL = (value) => {
      try {
        const url = new URL(String(value || '').trim());
        return url.protocol === 'http:' || url.protocol === 'https:';
      } catch (err) {
        return false;
      }
    };
    const validateConfigForm = () => {
      clearValidationState();
      const errors = [];
      const warnings = [];

      const healthPath = byId('cfgHealthPath');
      if (byId('cfgHealthEnabled')?.checked && !String(healthPath?.value || '').trim()) {
        errors.push('Health Check path is required when health checks are enabled.');
        markInvalid(healthPath, 'Path is required while health checks are enabled.');
      }

      const bridgeCards = Array.from(document.querySelectorAll('[data-bridge-rule]'));
      bridgeCards.forEach((card, idx) => {
        const from = card.querySelector('.bridge-from');
        const to = card.querySelector('.bridge-to');
        const fromValue = String(from?.value || '').trim();
        const toValue = String(to?.value || '').trim();
        if (!fromValue || !toValue) {
          errors.push('Bridge Rule ' + (idx + 1) + ' requires both From and To.');
          if (!fromValue) markInvalid(from, 'Bridge source model is required.');
          if (!toValue) markInvalid(to, 'Bridge target model is required.');
        }
      });

      const retryMin = byId('cfgRetryMin');
      const retryCodesInput = byId('cfgRetryCodes');
      const retryMinValue = readOptionalNumber(retryMin);
      if (retryMinValue !== null && retryMinValue < 100) {
        errors.push('Retry Policy status code min must be at least 100.');
        markInvalid(retryMin, 'Minimum retry status code must be between 100 and 599.');
      }
      parseList(retryCodesInput?.value).forEach((raw) => {
        const value = Number.parseInt(raw, 10);
        if (!Number.isFinite(value) || value < 100 || value > 599) {
          errors.push('Retry Policy contains an invalid status code: ' + raw + '.');
          markInvalid(retryCodesInput, 'Retry status codes must be integers between 100 and 599.');
        }
      });

      Array.from(document.querySelectorAll('[data-intercept]')).forEach((card, idx) => {
        const codesInput = card.querySelector('.intercept-codes');
        const minInput = card.querySelector('.intercept-min');
        parseList(codesInput?.value).forEach((raw) => {
          const value = Number.parseInt(raw, 10);
          if (!Number.isFinite(value) || value < 100 || value > 599) {
            errors.push('Response Intercept ' + (idx + 1) + ' contains an invalid status code: ' + raw + '.');
            markInvalid(codesInput, 'Intercept status codes must be integers between 100 and 599.');
          }
        });
        const minValue = readOptionalNumber(minInput);
        if (minValue !== null && minValue < 100) {
          errors.push('Response Intercept ' + (idx + 1) + ' status code min must be at least 100.');
          markInvalid(minInput, 'Intercept minimum status code must be between 100 and 599.');
        }
      });

      const upstreamCards = Array.from(document.querySelectorAll('[data-upstream-config]'));
      const seenNames = new Map();
      let enabledCount = 0;
      upstreamCards.forEach((card, idx) => {
        const nameInput = card.querySelector('.upstream-name');
        const baseURLInput = card.querySelector('.upstream-base-url');
        const keyInput = card.querySelector('.upstream-api-key');
        const modelsInput = card.querySelector('.upstream-models');
        const enabled = card.querySelector('.upstream-enabled')?.checked ?? true;
        const name = String(nameInput?.value || '').trim();
        const baseURL = String(baseURLInput?.value || '').trim();
        const models = parseList(modelsInput?.value);

        if (enabled) enabledCount++;
        if (!name) {
          errors.push('Service Provider ' + (idx + 1) + ' requires a provider name.');
          markInvalid(nameInput, 'Provider name is required.');
        } else {
          const key = name.toLowerCase();
          if (seenNames.has(key)) {
            errors.push('Duplicate provider name: ' + name + '.');
            markInvalid(nameInput, 'Provider name must be unique.');
            markInvalid(seenNames.get(key), 'Provider name must be unique.');
          } else {
            seenNames.set(key, nameInput);
          }
        }
        if (!baseURL || !isValidHTTPURL(baseURL)) {
          errors.push('Service Provider ' + (idx + 1) + ' requires a valid http/https Base URL.');
          markInvalid(baseURLInput, 'Base URL must start with http:// or https://.');
        }
        if (!String(keyInput?.value || '').trim()) {
          warnings.push('Service Provider ' + (idx + 1) + ' has an empty API key.');
          markInvalid(keyInput, 'API key is empty. Requests may fail if this upstream requires auth.', true);
        }
        if (!models.length) {
          warnings.push('Service Provider ' + (idx + 1) + ' has no model scope configured.');
          markInvalid(modelsInput, 'No models configured. This provider will not match model-based routing.', true);
        }
      });

      if (!upstreamCards.length) {
        errors.push('At least one service provider is required.');
      } else if (enabledCount === 0) {
        errors.push('At least one service provider must be enabled.');
      }

      renderValidationSummary(errors, warnings);
      return {
        valid: errors.length === 0,
        errors,
        warnings
      };
    };
    const renderBridgeRules = (rules) => {
      const list = byId('bridgeRuleList');
      const items = rules || [];
      if (!items.length) {
        list.innerHTML = '<div class="small">暂无桥接规则</div>';
        return;
      }
      list.innerHTML = items.map((rule, idx) => {
        return '' +
          '<div class="config-card" data-bridge-rule>' +
            '<div class="config-card-head">' +
              '<div class="config-card-title">Bridge Rule ' + (idx + 1) + '</div>' +
              '<div class="config-actions">' +
                '<button class="btn danger bridge-rule-remove" type="button">Remove</button>' +
              '</div>' +
            '</div>' +
            '<div class="config-grid">' +
              '<div class="config-field">' +
                '<label>From</label>' +
                '<input type="text" class="bridge-from" placeholder="gpt-5.2" value="' + escapeHTML(rule.from || '') + '">' +
              '</div>' +
              '<div class="config-field">' +
                '<label>To</label>' +
                '<input type="text" class="bridge-to" placeholder="gpt-5.4" value="' + escapeHTML(rule.to || '') + '">' +
              '</div>' +
            '</div>' +
          '</div>';
      }).join('');
    };
    const renderIntercepts = (rules) => {
      const list = byId('interceptList');
      const items = rules || [];
      if (!items.length) {
        list.innerHTML = '<div class="small">暂无拦截规则</div>';
        return;
      }
      list.innerHTML = items.map((rule, idx) => {
        const enabled = rule.enabled === false ? '' : 'checked';
        const action = String(rule.action).toLowerCase();
        return '' +
          '<div class="config-card" data-intercept>' +
            '<div class="config-card-head">' +
              '<div class="config-card-title">Rule ' + (idx + 1) + '</div>' +
              '<div class="config-actions">' +
                '<label class="small"><input type="checkbox" class="intercept-enabled" ' + enabled + '> Enabled</label>' +
                '<button class="btn danger intercept-remove" type="button">Remove</button>' +
              '</div>' +
            '</div>' +
            '<div class="config-grid">' +
              '<div class="config-field">' +
                '<label>Name</label>' +
                '<input type="text" class="intercept-name" value="' + escapeHTML(rule.name || '') + '">' +
              '</div>' +
              '<div class="config-field">' +
                '<label>Action</label>' +
                '<select class="intercept-action">' +
                  '<option value="retry" ' + (action === 'retry' ? 'selected' : '') + '>retry</option>' +
                  '<option value="fail" ' + (action === 'fail' ? 'selected' : '') + '>fail</option>' +
                '</select>' +
              '</div>' +
              '<div class="config-field">' +
                '<label>Paths</label>' +
                '<input type="text" class="intercept-paths" placeholder="/v1/responses, /v1/chat/*" value="' + escapeHTML(listToString(rule.paths || [])) + '">' +
              '</div>' +
              '<div class="config-field">' +
                '<label>Status Codes</label>' +
                '<input type="text" class="intercept-codes" placeholder="429,502" value="' + escapeHTML(listToString(rule.status_codes || [])) + '">' +
              '</div>' +
              '<div class="config-field">' +
                '<label>Status Code Min</label>' +
                '<input type="number" min="0" class="intercept-min" value="' + (rule.status_code_min ?? '') + '">' +
              '</div>' +
              '<div class="config-field" style="grid-column: span 2;">' +
                '<label>Message Keywords</label>' +
                '<textarea class="intercept-keywords" placeholder="upstream request failed">' + escapeHTML(keywordsToString(rule.message_keywords || [])) + '</textarea>' +
              '</div>' +
            '</div>' +
          '</div>';
      }).join('');
    };
    const collectIntercepts = () => {
      const cards = Array.from(document.querySelectorAll('[data-intercept]'));
      return cards.map(card => {
        const enabled = card.querySelector('.intercept-enabled')?.checked ?? true;
        const name = card.querySelector('.intercept-name')?.value || '';
        const action = card.querySelector('.intercept-action')?.value || 'fail';
        const paths = parseList(card.querySelector('.intercept-paths')?.value);
        const codes = parseCodes(card.querySelector('.intercept-codes')?.value);
        const min = readOptionalNumber(card.querySelector('.intercept-min'));
        const keywords = parseList(card.querySelector('.intercept-keywords')?.value);
        return {
          name,
          enabled,
          action,
          paths,
          status_codes: codes,
          status_code_min: min,
          message_keywords: keywords
        };
      });
    };
    const collectBridgeRules = () => {
      const cards = Array.from(document.querySelectorAll('[data-bridge-rule]'));
      return cards.map(card => ({
        from: card.querySelector('.bridge-from')?.value || '',
        to: card.querySelector('.bridge-to')?.value || ''
      }));
    };
    const updateSettingsSummary = () => {
      const providerCount = document.querySelectorAll('[data-upstream-config]').length;
      const healthPath = String(byId('cfgHealthPath')?.value || '').trim() || '/v1/models';
      if (byId('settingsProviderCount')) byId('settingsProviderCount').textContent = fmt.format(providerCount);
      if (byId('settingsHealthPath')) byId('settingsHealthPath').textContent = healthPath;
    };
    const collectUpstreamCard = (card) => ({
      name: card.querySelector('.upstream-name')?.value || '',
      base_url: card.querySelector('.upstream-base-url')?.value || '',
      api_key: card.querySelector('.upstream-api-key')?.value || '',
      models: parseList(card.querySelector('.upstream-models')?.value),
      headers: parseHeaders(card.querySelector('.upstream-headers')?.value),
      weight: readNumber(card.querySelector('.upstream-weight')),
      timeout_ms: readNumber(card.querySelector('.upstream-timeout')),
      same_upstream_retries: readNumber(card.querySelector('.upstream-same-retries')),
      enabled: card.querySelector('.upstream-enabled')?.checked ?? true
    });
    const renderUpstreamProbe = (card, result, pending = false) => {
      const host = card.querySelector('.probe-status-host');
      if (!host) return;
      if (pending) {
        host.innerHTML = '<div class="probe-status"><div class="probe-summary"><span class="status">testing</span><span class="small">正在探测 provider...</span></div></div>';
        return;
      }
      if (!result) {
        host.innerHTML = '';
        return;
      }
      const kind = result.ok ? 'ok' : 'fail';
      const state = result.ok ? '<span class="status">reachable</span>' : '<span class="status bad">failed</span>';
      const target = result.target_url ? '<div class="small">Target · ' + escapeHTML(result.target_url) + '</div>' : '';
      const preview = result.body_preview ? '<div class="probe-preview">' + escapeHTML(result.body_preview) + '</div>' : '';
      const detailBits = [
        result.status_code ? '<span class="tag">status ' + escapeHTML(result.status_code) + '</span>' : '',
        result.latency_ms ? '<span class="tag">' + escapeHTML(result.latency_ms) + ' ms</span>' : '',
        result.checked_at ? '<span class="tag">' + escapeHTML(relativeTime(result.checked_at)) + '</span>' : ''
      ].filter(Boolean).join('');
      host.innerHTML = '<div class="probe-status ' + kind + '">' +
        '<div class="probe-summary">' + state + detailBits + '</div>' +
        (result.error ? '<div class="small">' + escapeHTML(result.error) + '</div>' : '') +
        target +
        preview +
      '</div>';
    };
    const renderUpstreamsConfig = (upstreams) => {
      const list = byId('upstreamConfigList');
      const items = upstreams || [];
      if (!items.length) {
        list.innerHTML = '<div class="small">暂无服务商配置</div>';
        updateSettingsSummary();
        return;
      }
      list.innerHTML = items.map((upstream, idx) => {
        const enabled = upstream.enabled === false ? '' : 'checked';
        const title = escapeHTML(upstream.name || ('Provider ' + (idx + 1)));
        const base = escapeHTML(upstream.base_url || 'base url not set');
        return '' +
          '<div class="config-card" data-upstream-config>' +
            '<div class="config-card-head">' +
              '<div class="config-card-head-main">' +
                '<div class="config-card-title">' + title + '</div>' +
                '<div class="config-help">' + base + '</div>' +
              '</div>' +
              '<div class="config-actions">' +
                '<label class="small"><input type="checkbox" class="upstream-enabled" ' + enabled + '> Enabled</label>' +
                '<button class="btn secondary upstream-test" type="button">Probe</button>' +
                '<button class="btn danger upstream-remove" type="button">Remove</button>' +
              '</div>' +
            '</div>' +
            '<div class="config-grid">' +
              '<div class="config-field">' +
                '<label>Provider Name</label>' +
                '<input type="text" class="upstream-name" value="' + escapeHTML(upstream.name || '') + '">' +
              '</div>' +
              '<div class="config-field">' +
                '<label>Base URL</label>' +
                '<input type="text" class="upstream-base-url" placeholder="https://api.example.com" value="' + escapeHTML(upstream.base_url || '') + '">' +
              '</div>' +
              '<div class="config-field" style="grid-column: span 2;">' +
                '<label>API Key</label>' +
                '<input type="text" class="upstream-api-key" placeholder="sk-..." value="' + escapeHTML(upstream.api_key || '') + '">' +
              '</div>' +
              '<div class="config-field">' +
                '<label>Models</label>' +
                '<textarea class="upstream-models" placeholder="gpt-5.2&#10;gpt-5.2-codex">' + escapeHTML((upstream.models || []).join("\n")) + '</textarea>' +
              '</div>' +
              '<div class="config-field">' +
                '<label>Headers</label>' +
                '<textarea class="upstream-headers" placeholder="X-Org: demo&#10;X-Region: cn">' + escapeHTML(headersToString(upstream.headers || {})) + '</textarea>' +
              '</div>' +
              '<div class="config-field">' +
                '<label>Weight</label>' +
                '<input type="number" min="0" class="upstream-weight" value="' + (upstream.weight ?? 0) + '">' +
              '</div>' +
              '<div class="config-field">' +
                '<label>Timeout (ms)</label>' +
                '<input type="number" min="0" class="upstream-timeout" value="' + (upstream.timeout_ms ?? 0) + '">' +
              '</div>' +
              '<div class="config-field">' +
                '<label>Same Upstream Retries</label>' +
                '<input type="number" min="0" class="upstream-same-retries" value="' + (upstream.same_upstream_retries ?? 0) + '">' +
              '</div>' +
            '</div>' +
            '<div class="probe-status-host"></div>' +
          '</div>';
      }).join('');
      updateSettingsSummary();
    };
    const collectUpstreams = () => {
      const cards = Array.from(document.querySelectorAll('[data-upstream-config]'));
      return cards.map(card => collectUpstreamCard(card));
    };
    const renderConfigHistory = (versions) => {
      const list = byId('configHistoryList');
      const items = versions || [];
      if (!items.length) {
        list.innerHTML = '<div class="small">暂无历史版本</div>';
        byId('configDiffPreview').innerHTML = '';
        return;
      }
      list.innerHTML = '<div class="history-list">' + items.map((item, idx) => {
        return '' +
          '<div class="history-item">' +
            '<div class="history-meta">' +
              '<div class="history-name">' + escapeHTML(item.filename || item.id || ('Version ' + (idx + 1))) + '</div>' +
              '<div class="small">saved ' + escapeHTML(relativeTime(item.created_at)) + ' · ' + escapeHTML(fmtBytes(item.size)) + '</div>' +
            '</div>' +
            '<div class="config-actions">' +
              '<button class="btn secondary history-preview" type="button" data-version-id="' + escapeHTML(item.id || '') + '">Preview</button>' +
              '<button class="btn danger history-rollback" type="button" data-version-id="' + escapeHTML(item.id || '') + '">Rollback</button>' +
            '</div>' +
          '</div>';
      }).join('') + '</div>';
    };
    const renderConfigDiffPreview = (payload) => {
      const host = byId('configDiffPreview');
      if (!payload || !payload.lines || !payload.lines.length) {
        host.innerHTML = '';
        return;
      }
      const summary = payload.summary || {};
      host.innerHTML = '' +
        '<div class="config-card" style="margin-top: 12px;">' +
          '<div class="config-card-head">' +
            '<div class="config-card-title">Diff Preview</div>' +
            '<div class="config-help">' + escapeHTML(payload.version?.filename || '') + '</div>' +
          '</div>' +
          '<div class="diff-summary">' +
            '<span class="tag accent">+' + escapeHTML(summary.added_lines || 0) + ' added</span>' +
            '<span class="tag">' + escapeHTML(summary.removed_lines || 0) + ' removed</span>' +
            '<span class="tag">' + escapeHTML(summary.changed_blocks || 0) + ' changed blocks</span>' +
          '</div>' +
          '<div class="diff-shell"><div class="diff-lines">' +
            payload.lines.map((line) => {
              const kind = String(line.kind || 'context');
              const mark = kind === 'add' ? '+' : (kind === 'remove' ? '-' : ' ');
              return '<div class="diff-line ' + escapeHTML(kind) + '"><div class="diff-mark">' + mark + '</div><div>' + escapeHTML(line.text || '') + '</div></div>';
            }).join('') +
          '</div></div>' +
        '</div>';
    };
    const loadConfigHistory = async () => {
      try {
        const res = await fetch('/-/admin/config/history', { cache: 'no-store' });
        if (!res.ok) {
          renderConfigHistory([]);
          return;
        }
        const payload = await res.json();
        renderConfigHistory(payload.versions || []);
      } catch (err) {
        renderConfigHistory([]);
      }
    };
    const loadConfigDiff = async (versionId) => {
      if (!versionId) {
        renderConfigDiffPreview(null);
        return;
      }
      byId('configHint').textContent = 'Loading diff...';
      try {
        const res = await fetch('/-/admin/config/history/' + encodeURIComponent(versionId) + '/diff', { cache: 'no-store' });
        if (!res.ok) {
          const text = await res.text();
          byId('configHint').textContent = text || 'Load diff failed';
          return;
        }
        const payload = await res.json();
        renderConfigDiffPreview(payload);
        byId('configHint').textContent = 'Diff loaded';
      } catch (err) {
        byId('configHint').textContent = 'Load diff failed';
      }
    };
    const loadConfig = async () => {
      try {
        const res = await fetch('/-/admin/config', { cache: 'no-store' });
        if (!res.ok) {
          byId('configStatus').textContent = '配置载入失败';
          return;
        }
        const cfg = await res.json();
        byId('cfgHealthEnabled').checked = !!cfg.health?.enabled;
        byId('cfgHealthInterval').value = cfg.health?.interval_sec ?? 0;
        byId('cfgHealthTimeout').value = cfg.health?.timeout_ms ?? 0;
        byId('cfgHealthPath').value = cfg.health?.path || '';

        byId('cfgBridgeEnabled').checked = !!cfg.bridge?.enabled;
        byId('cfgBridgeExcludeUA').value = keywordsToString(cfg.bridge?.exclude_user_agents || []);
        renderBridgeRules(cfg.bridge?.rules || []);

        byId('cfgMaxRetries').value = cfg.router?.max_retries ?? 0;
        byId('cfgBackoff').value = cfg.router?.retry_backoff_ms ?? 0;
        byId('cfgBackoffMax').value = cfg.router?.retry_backoff_max_ms ?? 0;
        byId('cfgFailureThreshold').value = cfg.router?.failure_threshold ?? 0;
        byId('cfgCooldown').value = cfg.router?.cooldown_sec ?? 0;
        byId('cfgPassthrough').value = cfg.router?.failure_passthrough_after_sec ?? 0;

        const retry = cfg.proxy?.retry || {};
        byId('cfgRetryCodes').value = listToString(retry.status_codes || []);
        byId('cfgRetryMin').value = retry.status_code_min ?? '';
        byId('cfgRetryKeywords').value = keywordsToString(retry.message_keywords || []);

        renderIntercepts(cfg.proxy?.intercepts || []);
        renderUpstreamsConfig(cfg.upstreams || []);
        await loadConfigHistory();
        ensureSectionControls();
        applyConfigSearch();
        clearValidationState();
        updateSettingsSummary();
        byId('configStatus').textContent = '已载入';
      } catch (err) {
        byId('configStatus').textContent = '配置载入失败';
      }
    };
    const testUpstreamCard = async (card) => {
      if (!card) return;
      renderUpstreamProbe(card, null, true);
      byId('configHint').textContent = 'Testing provider...';
      try {
        const res = await fetch('/-/admin/upstreams/test', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ upstream: collectUpstreamCard(card) })
        });
        const payload = await res.json().catch(() => ({}));
        renderUpstreamProbe(card, payload, false);
        byId('configHint').textContent = payload.ok ? 'Provider test passed' : 'Provider test failed';
      } catch (err) {
        renderUpstreamProbe(card, { ok: false, error: String(err?.message || err || 'probe failed') }, false);
        byId('configHint').textContent = 'Provider test failed';
      }
    };
    const saveConfig = async () => {
      const validation = validateConfigForm();
      if (!validation.valid) {
        byId('configHint').textContent = 'Fix validation errors before saving';
        return;
      }
      const payload = {
        health: {
          enabled: byId('cfgHealthEnabled').checked,
          interval_sec: readNumber(byId('cfgHealthInterval')),
          timeout_ms: readNumber(byId('cfgHealthTimeout')),
          path: byId('cfgHealthPath').value || ''
        },
        bridge: {
          enabled: byId('cfgBridgeEnabled').checked,
          exclude_user_agents: parseList(byId('cfgBridgeExcludeUA').value),
          rules: collectBridgeRules()
        },
        router: {
          max_retries: readNumber(byId('cfgMaxRetries')),
          retry_backoff_ms: readNumber(byId('cfgBackoff')),
          retry_backoff_max_ms: readNumber(byId('cfgBackoffMax')),
          failure_threshold: readNumber(byId('cfgFailureThreshold')),
          cooldown_sec: readNumber(byId('cfgCooldown')),
          failure_passthrough_after_sec: readNumber(byId('cfgPassthrough'))
        },
        proxy: {
          retry: {
            status_codes: parseCodes(byId('cfgRetryCodes').value),
            status_code_min: readOptionalNumber(byId('cfgRetryMin')),
            message_keywords: parseList(byId('cfgRetryKeywords').value)
          },
          intercepts: collectIntercepts()
        },
        upstreams: collectUpstreams()
      };

      byId('configHint').textContent = 'Saving...';
      try {
        const res = await fetch('/-/admin/config', {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(payload)
        });
        if (!res.ok) {
          const text = await res.text();
          byId('configHint').textContent = text || 'Save failed';
          return;
        }
        await loadConfig();
        byId('configHint').textContent = 'Saved';
      } catch (err) {
        byId('configHint').textContent = 'Save failed';
      }
    };
    const exportConfig = () => {
      window.open('/-/admin/config/export', '_blank', 'noopener,noreferrer');
    };
    const rollbackConfig = async (versionId = '') => {
      const confirmText = versionId
        ? 'Rollback to the selected config version?'
        : 'Rollback to the latest saved config version?';
      if (!window.confirm(confirmText)) {
        return;
      }
      byId('configHint').textContent = 'Rolling back...';
      try {
        const res = await fetch('/-/admin/config/rollback', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ version_id: versionId })
        });
        if (!res.ok) {
          const text = await res.text();
          byId('configHint').textContent = text || 'Rollback failed';
          return;
        }
        await loadConfig();
        byId('configHint').textContent = 'Rolled back';
      } catch (err) {
        byId('configHint').textContent = 'Rollback failed';
      }
    };
    document.addEventListener('click', (event) => {
      if (event.target && event.target.id === 'addBridgeRule') {
        event.preventDefault();
        const current = collectBridgeRules();
        renderBridgeRules([...current, { from: '', to: '' }]);
        clearValidationState();
      }
      if (event.target && event.target.id === 'addIntercept') {
        event.preventDefault();
        const current = collectIntercepts();
        renderIntercepts([...current, { name: '', enabled: true, action: 'fail', paths: [], status_codes: [], status_code_min: null, message_keywords: [] }]);
        clearValidationState();
      }
      if (event.target && event.target.id === 'addUpstream') {
        event.preventDefault();
        const current = collectUpstreams();
        renderUpstreamsConfig([...current, { name: '', base_url: '', api_key: '', models: [], headers: {}, weight: 1, timeout_ms: 30000, same_upstream_retries: 0, enabled: true }]);
        clearValidationState();
      }
      if (event.target && event.target.classList.contains('intercept-remove')) {
        event.preventDefault();
        const card = event.target.closest('[data-intercept]');
        if (card) {
          card.remove();
          if (!document.querySelector('[data-intercept]')) {
            byId('interceptList').innerHTML = '<div class="small">暂无拦截规则</div>';
          }
          clearValidationState();
        }
      }
      if (event.target && event.target.classList.contains('bridge-rule-remove')) {
        event.preventDefault();
        const card = event.target.closest('[data-bridge-rule]');
        if (card) {
          card.remove();
          if (!document.querySelector('[data-bridge-rule]')) {
            byId('bridgeRuleList').innerHTML = '<div class="small">暂无桥接规则</div>';
          }
          clearValidationState();
        }
      }
      if (event.target && event.target.classList.contains('upstream-remove')) {
        event.preventDefault();
        const card = event.target.closest('[data-upstream-config]');
        if (card) {
          card.remove();
          if (!document.querySelector('[data-upstream-config]')) {
            byId('upstreamConfigList').innerHTML = '<div class="small">暂无服务商配置</div>';
          }
          updateSettingsSummary();
          clearValidationState();
        }
      }
      if (event.target && event.target.classList.contains('upstream-test')) {
        event.preventDefault();
        testUpstreamCard(event.target.closest('[data-upstream-config]'));
      }
      if (event.target && event.target.id === 'collapseSections') {
        event.preventDefault();
        topLevelSections().forEach((section) => setSectionCollapsed(section, true));
      }
      if (event.target && event.target.id === 'expandSections') {
        event.preventDefault();
        topLevelSections().forEach((section) => setSectionCollapsed(section, false));
      }
      if (event.target && event.target.classList.contains('section-toggle')) {
        event.preventDefault();
        const section = event.target.closest('.config-section');
        if (section) {
          setSectionCollapsed(section, !section.classList.contains('collapsed'));
        }
      }
      if (event.target && event.target.id === 'saveConfig') {
        event.preventDefault();
        saveConfig();
      }
      if (event.target && event.target.id === 'reloadConfig') {
        event.preventDefault();
        loadConfig();
      }
      if (event.target && event.target.id === 'exportConfig') {
        event.preventDefault();
        exportConfig();
      }
      if (event.target && event.target.id === 'rollbackConfig') {
        event.preventDefault();
        rollbackConfig();
      }
      if (event.target && event.target.classList.contains('history-rollback')) {
        event.preventDefault();
        rollbackConfig(event.target.getAttribute('data-version-id') || '');
      }
      if (event.target && event.target.classList.contains('history-preview')) {
        event.preventDefault();
        loadConfigDiff(event.target.getAttribute('data-version-id') || '');
      }
    });
    document.addEventListener('input', (event) => {
      if (event.target && event.target.id === 'configSearch') {
        applyConfigSearch();
        return;
      }
      if (event.target && (event.target.id === 'cfgHealthPath' || event.target.closest('[data-upstream-config]'))) {
        updateSettingsSummary();
      }
      if (event.target && event.target.closest('.config-panel')) {
        clearValidationState();
      }
    });
    ensureSectionControls();
    revealSettingsIfHash();

    /* ── Chart engine (pure SVG, no external lib) ── */
    const CHART_COLORS = ['#7ee7d6','#f1b866','#a78bfa','#f87171','#38bdf8','#4ade80','#fb923c','#e879f9'];
    const chartState = { hours: 24, bucket: 60 };

    const svgNS = 'http://www.w3.org/2000/svg';
    const svgEl = (tag, attrs = {}) => {
      const el = document.createElementNS(svgNS, tag);
      for (const [k, v] of Object.entries(attrs)) el.setAttribute(k, v);
      return el;
    };

    const shortTime = (iso) => {
      const d = new Date(iso);
      if (isNaN(d)) return iso;
      const hh = String(d.getHours()).padStart(2, '0');
      const mm = String(d.getMinutes()).padStart(2, '0');
      if (chartState.hours <= 24) return hh + ':' + mm;
      return (d.getMonth() + 1) + '/' + d.getDate() + ' ' + hh + ':' + mm;
    };

    const setupTooltip = (wrapId, tipId) => {
      const wrap = byId(wrapId);
      const tip = byId(tipId);
      if (!wrap || !tip) return { show() {}, hide() {} };
      return {
        show(x, y, html) {
          tip.innerHTML = html;
          tip.classList.add('visible');
          const rect = wrap.getBoundingClientRect();
          let left = x + 12;
          if (left + tip.offsetWidth > rect.width - 4) left = x - tip.offsetWidth - 8;
          let top = y - 8;
          if (top < 0) top = 4;
          tip.style.left = left + 'px';
          tip.style.top = top + 'px';
        },
        hide() { tip.classList.remove('visible'); }
      };
    };

    const renderLegend = (id, items) => {
      const el = byId(id);
      if (!el) return;
      el.innerHTML = items.map(([label, color]) =>
        '<span class="chart-legend-item"><span class="chart-legend-dot" style="background:' + color + '"></span>' + escapeHTML(label) + '</span>'
      ).join('');
    };

    /* Line / area chart renderer */
    const drawLineChart = (wrapId, tipId, series, opts = {}) => {
      const wrap = byId(wrapId);
      if (!wrap) return;
      const tip = setupTooltip(wrapId, tipId);
      const W = wrap.clientWidth || 600;
      const H = wrap.clientHeight || 220;
      const pad = { top: 16, right: 16, bottom: 28, left: 52 };
      const cw = W - pad.left - pad.right;
      const ch = H - pad.top - pad.bottom;

      // find global max
      let maxVal = 0;
      for (const s of series) {
        for (const v of s.values) if (v > maxVal) maxVal = v;
      }
      if (maxVal === 0) maxVal = 1;
      const labels = series[0]?.labels || [];
      const n = labels.length;
      if (n === 0) { wrap.querySelector('svg')?.remove(); return; }

      const svg = svgEl('svg', { viewBox: '0 0 ' + W + ' ' + H, preserveAspectRatio: 'none' });

      // grid lines
      const gridSteps = 4;
      for (let i = 0; i <= gridSteps; i++) {
        const y = pad.top + ch - (ch / gridSteps) * i;
        svg.appendChild(svgEl('line', { x1: pad.left, y1: y, x2: W - pad.right, y2: y, stroke: 'rgba(255,255,255,0.06)', 'stroke-width': 1 }));
        const label = svgEl('text', { x: pad.left - 6, y: y + 4, fill: '#b4a99a', 'font-size': '10', 'text-anchor': 'end', 'font-family': 'inherit' });
        const val = (maxVal / gridSteps) * i;
        label.textContent = val >= 1000000 ? (val / 1000000).toFixed(1) + 'M' : val >= 1000 ? (val / 1000).toFixed(1) + 'K' : Math.round(val);
        svg.appendChild(label);
      }

      // x-axis labels
      const labelStep = Math.max(1, Math.floor(n / 6));
      for (let i = 0; i < n; i += labelStep) {
        const x = pad.left + (i / Math.max(1, n - 1)) * cw;
        const label = svgEl('text', { x: x, y: H - 4, fill: '#b4a99a', 'font-size': '10', 'text-anchor': 'middle', 'font-family': 'inherit' });
        label.textContent = shortTime(labels[i]);
        svg.appendChild(label);
      }

      // draw series
      series.forEach((s, si) => {
        const color = s.color || CHART_COLORS[si % CHART_COLORS.length];
        const points = s.values.map((v, i) => {
          const x = pad.left + (i / Math.max(1, n - 1)) * cw;
          const y = pad.top + ch - (v / maxVal) * ch;
          return [x, y];
        });
        const pathD = points.map((p, i) => (i === 0 ? 'M' : 'L') + p[0].toFixed(1) + ',' + p[1].toFixed(1)).join(' ');

        if (opts.area) {
          const areaD = pathD + ' L' + points[points.length - 1][0].toFixed(1) + ',' + (pad.top + ch) + ' L' + points[0][0].toFixed(1) + ',' + (pad.top + ch) + ' Z';
          svg.appendChild(svgEl('path', { d: areaD, fill: color, opacity: '0.12' }));
        }
        svg.appendChild(svgEl('path', { d: pathD, fill: 'none', stroke: color, 'stroke-width': '2', 'stroke-linejoin': 'round', 'stroke-linecap': 'round' }));

        // dots
        points.forEach((p) => {
          svg.appendChild(svgEl('circle', { cx: p[0], cy: p[1], r: '2.5', fill: color, opacity: '0.7' }));
        });
      });

      // hover overlay
      const overlay = svgEl('rect', { x: pad.left, y: pad.top, width: cw, height: ch, fill: 'transparent' });
      overlay.addEventListener('mousemove', (e) => {
        const rect = wrap.getBoundingClientRect();
        const mx = e.clientX - rect.left;
        const idx = Math.round(((mx - pad.left) / cw) * (n - 1));
        if (idx < 0 || idx >= n) { tip.hide(); return; }
        let html = '<strong>' + shortTime(labels[idx]) + '</strong>';
        series.forEach((s, si) => {
          const color = s.color || CHART_COLORS[si % CHART_COLORS.length];
          const val = s.values[idx] ?? 0;
          html += '<br><span style="color:' + color + '">' + escapeHTML(s.name) + '</span>: ' + (opts.fmtVal ? opts.fmtVal(val) : fmt.format(Math.round(val)));
        });
        tip.show(mx, e.clientY - rect.top, html);
      });
      overlay.addEventListener('mouseleave', () => tip.hide());
      svg.appendChild(overlay);

      // replace
      const old = wrap.querySelector('svg');
      if (old) old.remove();
      wrap.insertBefore(svg, wrap.firstChild);
    };

    /* Stacked bar chart renderer */
    const drawStackedBarChart = (wrapId, tipId, labels, stackData, upstreamNames) => {
      const wrap = byId(wrapId);
      if (!wrap) return;
      const tip = setupTooltip(wrapId, tipId);
      const W = wrap.clientWidth || 600;
      const H = wrap.clientHeight || 220;
      const pad = { top: 16, right: 16, bottom: 28, left: 52 };
      const cw = W - pad.left - pad.right;
      const ch = H - pad.top - pad.bottom;
      const n = labels.length;
      if (n === 0) { wrap.querySelector('svg')?.remove(); return; }

      // compute totals per bucket
      const totals = labels.map((_, i) => {
        let sum = 0;
        for (const name of upstreamNames) sum += (stackData[i]?.[name] || 0);
        return sum;
      });
      let maxVal = Math.max(...totals);
      if (maxVal === 0) maxVal = 1;

      const svg = svgEl('svg', { viewBox: '0 0 ' + W + ' ' + H, preserveAspectRatio: 'none' });

      // grid
      const gridSteps = 4;
      for (let i = 0; i <= gridSteps; i++) {
        const y = pad.top + ch - (ch / gridSteps) * i;
        svg.appendChild(svgEl('line', { x1: pad.left, y1: y, x2: W - pad.right, y2: y, stroke: 'rgba(255,255,255,0.06)', 'stroke-width': 1 }));
        const label = svgEl('text', { x: pad.left - 6, y: y + 4, fill: '#b4a99a', 'font-size': '10', 'text-anchor': 'end', 'font-family': 'inherit' });
        const val = (maxVal / gridSteps) * i;
        label.textContent = val >= 1000000 ? (val / 1000000).toFixed(1) + 'M' : val >= 1000 ? (val / 1000).toFixed(1) + 'K' : Math.round(val);
        svg.appendChild(label);
      }

      // x-axis labels
      const labelStep = Math.max(1, Math.floor(n / 6));
      for (let i = 0; i < n; i += labelStep) {
        const x = pad.left + (i + 0.5) / n * cw;
        const label = svgEl('text', { x: x, y: H - 4, fill: '#b4a99a', 'font-size': '10', 'text-anchor': 'middle', 'font-family': 'inherit' });
        label.textContent = shortTime(labels[i]);
        svg.appendChild(label);
      }

      // bars
      const barW = Math.max(2, (cw / n) * 0.7);
      const gap = (cw / n) - barW;
      labels.forEach((_, i) => {
        const bx = pad.left + i * (cw / n) + gap / 2;
        let yOffset = 0;
        upstreamNames.forEach((name, ui) => {
          const val = stackData[i]?.[name] || 0;
          if (val <= 0) return;
          const barH = (val / maxVal) * ch;
          const by = pad.top + ch - yOffset - barH;
          const color = CHART_COLORS[ui % CHART_COLORS.length];
          svg.appendChild(svgEl('rect', { x: bx, y: by, width: barW, height: barH, fill: color, rx: '2', opacity: '0.85' }));
          yOffset += barH;
        });
      });

      // hover
      const overlay = svgEl('rect', { x: pad.left, y: pad.top, width: cw, height: ch, fill: 'transparent' });
      overlay.addEventListener('mousemove', (e) => {
        const rect = wrap.getBoundingClientRect();
        const mx = e.clientX - rect.left;
        const idx = Math.floor(((mx - pad.left) / cw) * n);
        if (idx < 0 || idx >= n) { tip.hide(); return; }
        let html = '<strong>' + shortTime(labels[idx]) + '</strong>';
        upstreamNames.forEach((name, ui) => {
          const val = stackData[idx]?.[name] || 0;
          if (val <= 0) return;
          const color = CHART_COLORS[ui % CHART_COLORS.length];
          html += '<br><span style="color:' + color + '">' + escapeHTML(name) + '</span>: ' + fmt.format(val);
        });
        html += '<br>Total: ' + fmt.format(totals[idx]);
        tip.show(mx, e.clientY - rect.top, html);
      });
      overlay.addEventListener('mouseleave', () => tip.hide());
      svg.appendChild(overlay);

      const old = wrap.querySelector('svg');
      if (old) old.remove();
      wrap.insertBefore(svg, wrap.firstChild);
    };

    /* Load and render charts */
    const loadCharts = async () => {
      try {
        const res = await fetch('/-/admin/timeseries?hours=' + chartState.hours + '&bucket=' + chartState.bucket, { cache: 'no-store' });
        if (!res.ok) return;
        const ts = await res.json();
        const buckets = ts.buckets || [];
        const byUp = ts.by_upstream || [];
        if (!buckets.length) return;

        const labels = buckets.map(b => b.t);
        const bucketMin = chartState.bucket;

        // RPM / TPM chart
        const rpmVals = buckets.map(b => b.requests / (bucketMin / 60));
        const tpmVals = buckets.map(b => b.total_tokens / (bucketMin / 60));
        drawLineChart('chartRpm', 'tipRpm', [
          { name: 'RPM', values: rpmVals, labels, color: CHART_COLORS[0] },
          { name: 'TPM (÷1K)', values: tpmVals.map(v => v / 1000), labels, color: CHART_COLORS[1] },
        ], { area: true, fmtVal: (v) => v.toFixed(1) });
        renderLegend('legendRpm', [['RPM', CHART_COLORS[0]], ['TPM (÷1K)', CHART_COLORS[1]]]);

        // Latency / success rate chart
        const latVals = buckets.map(b => b.avg_latency_ms);
        const srVals = buckets.map(b => b.requests > 0 ? (b.successes / b.requests) * 100 : 0);
        drawLineChart('chartLatency', 'tipLatency', [
          { name: 'Avg Latency (ms)', values: latVals, labels, color: CHART_COLORS[3] },
          { name: 'Success %', values: srVals, labels, color: CHART_COLORS[0] },
        ], { area: false, fmtVal: (v) => v.toFixed(1) });
        renderLegend('legendLatency', [['Avg Latency (ms)', CHART_COLORS[3]], ['Success %', CHART_COLORS[0]]]);

        // Token by upstream stacked bar
        const allUpstreams = new Set();
        byUp.forEach(b => { for (const k of Object.keys(b.upstreams || {})) allUpstreams.add(k); });
        const upNames = Array.from(allUpstreams);
        const stackData = byUp.map(b => b.upstreams || {});
        drawStackedBarChart('chartTokens', 'tipTokens', labels, stackData, upNames);
        renderLegend('legendTokens', upNames.map((n, i) => [n, CHART_COLORS[i % CHART_COLORS.length]]));

        // Success / failure stacked bar
        const sfLabels = labels;
        const sfStack = buckets.map(b => ({ 'Success': b.successes, 'Failure': b.failures }));
        drawStackedBarChart('chartSuccess', 'tipSuccess', sfLabels, sfStack, ['Success', 'Failure']);
        renderLegend('legendSuccess', [['Success', CHART_COLORS[0]], ['Failure', CHART_COLORS[3]]]);
      } catch (err) {
        // silently ignore chart load errors
      }
    };

    // Chart range controls
    document.getElementById('chartRangeControls')?.addEventListener('click', (e) => {
      const btn = e.target.closest('button[data-hours]');
      if (!btn) return;
      chartState.hours = parseInt(btn.dataset.hours, 10);
      chartState.bucket = parseInt(btn.dataset.bucket, 10);
      document.querySelectorAll('#chartRangeControls button').forEach(b => b.classList.remove('active'));
      btn.classList.add('active');
      loadCharts();
    });

    async function load() {
      const res = await fetch('/-/admin/data', { cache: 'no-store' });
      const data = await res.json();
      const summary = data.telemetry.summary || {};
      const perf = data.telemetry.performance || {};
      const oneMinute = perf.last_1m || {};
      const fiveMinute = perf.last_5m || {};
      const cacheTrends = data.telemetry.cache_trends || {};
      const oneHour = cacheTrends.last_1h || {};
      const dayWindow = cacheTrends.last_24h || {};
      const pricing = data.pricing || {};
      const pricingSummary = pricing.summary || {};
      const pricingModels = pricing.models || [];

      document.getElementById('generatedAt').textContent = 'Updated ' + new Date(data.generated_at).toLocaleString();
      document.getElementById('pricingSource').innerHTML = pricing.source_url
        ? 'Official pricing: <a href="' + pricing.source_url + '" target="_blank" rel="noreferrer">OpenAI</a>' + (pricing.updated_at ? ' · ' + relativeTime(pricing.updated_at) : '')
        : 'Official pricing unavailable';
      document.getElementById('bridgeState').textContent = 'Bridge ' + ((data.bridge && data.bridge.Enabled) ? 'ON' : 'OFF');

      document.getElementById('heroStats').innerHTML = [
        ['Success Rate', fmtPct((summary.total_requests || 0) > 0 ? ((summary.successes || 0) / summary.total_requests) * 100 : 0)],
        ['Total Cost', fmtMoney(pricingSummary.total_usd || 0)],
        ['1m RPM', fmtRate(oneMinute.rpm)],
        ['1m TPM', fmtRate(oneMinute.tpm)],
      ].map(([k, v]) => '<div class="hero-stat"><div class="k">' + k + '</div><div class="v mono">' + v + '</div></div>').join('');

      document.getElementById('metrics').innerHTML = [
        ['Requests', fmt.format(summary.total_requests || 0), fmt.format(summary.successes || 0) + ' success / ' + fmt.format(summary.failures || 0) + ' fail'],
        ['Total Tokens', fmt.format(summary.total_tokens || 0), fmt.format(summary.prompt_tokens || 0) + ' prompt / ' + fmt.format(summary.completion_tokens || 0) + ' completion'],
        ['1m Window', fmtRate(oneMinute.rpm) + ' RPM', fmtRate(oneMinute.tpm) + ' TPM'],
        ['5m Window', fmtRate(fiveMinute.rpm) + ' RPM', fmtRate(fiveMinute.tpm) + ' TPM'],
        ['1m Success', fmtPct(oneMinute.success_rate), fmtMs(oneMinute.avg_latency_ms)],
        ['5m Success', fmtPct(fiveMinute.success_rate), fmtMs(fiveMinute.avg_latency_ms)],
        ['1m Requests', fmt.format(oneMinute.requests || 0), fmt.format(oneMinute.failures || 0) + ' failures'],
        ['5m Requests', fmt.format(fiveMinute.requests || 0), fmt.format(fiveMinute.failures || 0) + ' failures'],
      ].map(([k, v, small]) => '<div class="metric"><div class="k">' + k + '</div><div class="v mono">' + v + '</div><div class="small">' + small + '</div></div>').join('');

      document.getElementById('costMetrics').innerHTML = [
        ['Estimated Cost', fmtMoney(pricingSummary.total_usd || 0), fmtMoney(pricingSummary.prompt_usd || 0) + ' input / ' + fmtMoney(pricingSummary.completion_usd || 0) + ' output'],
        ['Priced Models', fmt.format(pricingSummary.priced_models || 0), fmt.format(pricingSummary.unpriced_models || 0) + ' unpriced'],
        ['Cached Prompt', fmt.format(pricingSummary.cached_prompt_tokens || 0), fmtMoney(pricingSummary.cache_savings_usd || 0) + ' saved'],
        ['Cache Hit', cacheHitRate(summary) === null ? 'n/a' : fmtPct(cacheHitRate(summary)), fmt.format(summary.cached_prompt_tokens || 0) + ' / ' + fmt.format(summary.prompt_tokens || 0) + ' prompt tokens'],
      ].map(([k, v, small]) => '<div class="metric"><div class="k">' + k + '</div><div class="v mono">' + v + '</div><div class="small">' + small + '</div></div>').join('');

      const upstreamRows = Object.entries(data.upstreams || {}).map(([name, status]) => {
        const cooldown = status.cooldown_until && status.cooldown_until !== '0001-01-01T00:00:00Z' ? relativeTime(status.cooldown_until) : '-';
        const latency = status.last_latency ? fmtMs((status.last_latency || 0) / 1000000) : '-';
        return [name, statusPill(status.healthy), latency, status.consecutive_retryable_failures || 0, status.last_error || '-', cooldown];
      });
      document.getElementById('upstreams').innerHTML = table(['Upstream', 'State', 'Last Latency', 'Retryable Failures', 'Last Error', 'Cooldown'], upstreamRows, 'table-health');

      const modelRows = pricingModels
        .map((item) => [
          '<strong>' + escapeHTML(item.display_model || '-') + '</strong><br>'
            + (item.pricing_model && item.pricing_model !== item.display_model ? '<span class="small">priced as ' + escapeHTML(item.pricing_model) + '</span><br>' : '')
            + priceLine(item.pricing),
          promptUsageCell(item.usage),
          cacheRateCell(item.usage),
          fmt.format(item.usage.completion_tokens || 0),
          totalUsageCell(item.usage),
          fmtMoney(item.cost.total_usd || 0),
        ]);
      document.getElementById('byModel').innerHTML = table(['Model', 'Prompt', 'Cache Hit', 'Completion', 'Total', 'USD'], modelRows, 'table-models');

      const upstreamUsageRows = Object.entries(data.telemetry.by_upstream || {}).map(([name, usage]) => [
        name,
        promptUsageCell(usage),
        cacheRateCell(usage),
        fmt.format(usage.completion_tokens || 0),
        totalUsageCell(usage),
      ]);
      document.getElementById('byUpstream').innerHTML = table(['Upstream', 'Prompt', 'Cache Hit', 'Completion', 'Total'], upstreamUsageRows, 'table-usage');

      document.getElementById('cacheTrends').innerHTML = [
        ['1h Cache Hit', cacheHitRate(oneHour) === null ? 'n/a' : fmtPct(cacheHitRate(oneHour)), cacheTrendDetail(oneHour)],
        ['24h Cache Hit', cacheHitRate(dayWindow) === null ? 'n/a' : fmtPct(cacheHitRate(dayWindow)), cacheTrendDetail(dayWindow)],
      ].map(([k, v, small]) => '<div class="metric"><div class="k">' + k + '</div><div class="v mono">' + v + '</div><div class="small">' + small + '</div></div>').join('');

      const cacheRankingRows = (data.telemetry.cache_hit_ranking || []).map((item, idx) => [
        (idx + 1) + '. ' + escapeHTML(item.upstream || '-'),
        cacheRateCell(item.usage),
        fmt.format((item.usage && item.usage.cached_prompt_tokens) || 0),
        fmt.format((item.usage && item.usage.prompt_tokens) || 0),
        fmt.format(item.requests || 0),
      ]);
      document.getElementById('cacheRanking').innerHTML = table(['Upstream', 'Cache Hit', 'Cached', 'Prompt', 'Requests'], cacheRankingRows, 'table-cache');

      document.getElementById('errors').innerHTML = errorFeed(data.telemetry.errors || []);
      document.getElementById('requests').innerHTML = table(
        ['Time', 'Path', 'Model Flow', 'Route', 'Upstream', 'Status', 'Attempts', 'Latency', 'Cached Prompt', 'Cache Hit', 'Tokens', 'USD'],
        (data.telemetry.requests || []).slice(0, 24).map(item => [
          relativeTime(item.timestamp),
          item.path,
          modelFlow(item),
          '<span class="tag accent">' + escapeHTML(item.route_mode || 'direct') + '</span>',
          item.upstream || '-',
          item.status_code,
          item.attempts,
          fmtMs(item.duration_ms || 0),
          fmt.format((item.usage && item.usage.cached_prompt_tokens) || 0),
          cacheRateCell(item.usage),
          totalUsageCell(item.usage),
          estimateRequestCost(item, pricing) || '<span class="small">n/a</span>',
        ]),
        'table-requests'
      );
    }
    const settingsView = document.body.classList.contains('page-settings');
    if (settingsView) {
      loadConfig();
    } else {
      load();
      loadCharts();
      setInterval(load, 5000);
      setInterval(loadCharts, 15000);
    }
  </script>
</body>
</html>`

func renderAdminHTML(settingsView bool) string {
	bodyClass := ""
	settingsHref := "/admin/settings"
	settingsLabel := "Settings"
	if settingsView {
		bodyClass = "page-settings"
		settingsHref = "/admin"
		settingsLabel = "Overview"
	}
	return strings.NewReplacer(
		"{{BODY_CLASS}}", bodyClass,
		"{{SETTINGS_HREF}}", settingsHref,
		"{{SETTINGS_LABEL}}", settingsLabel,
	).Replace(adminHTMLTemplate)
}

func adminPage(settingsView bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(renderAdminHTML(settingsView)))
	}
}

func adminFavicon() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		_, _ = w.Write([]byte(adminIconSVG))
	}
}
