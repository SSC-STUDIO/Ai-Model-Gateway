package server

import (
	"net/http"
	"strings"

	"ai-model-gateway/internal/config"
	"ai-model-gateway/internal/router"
)

const adminIconSVG = `<svg width="256" height="256" viewBox="0 0 96 96" fill="none" xmlns="http://www.w3.org/2000/svg"><rect width="96" height="96" rx="24" fill="#0B0C0C"/><path d="M24 68V28H38L48 52L58 28H72V68H62V46L54 66H42L34 46V68H24Z" fill="#7EE7D6"/><circle cx="73" cy="24" r="8" fill="#F1B866"/></svg>`

const adminHTMLTemplate = `<!doctype html>
<html lang="{{HTML_LANG}}">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta name="theme-color" content="#0b0c0c">
  <title>{{PAGE_TITLE}}</title>
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
      width: 100%;
      margin: clamp(14px, 1.8vw, 24px) auto clamp(20px, 4vw, 56px);
      padding-inline: var(--page-gutter);
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
      position: sticky;
      top: 10px;
      z-index: 20;
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
      padding: 6px 10px;
      border-radius: 999px;
      border: 1px solid var(--line);
      background: rgba(255,255,255,0.04);
      color: var(--muted);
      font-size: 11px;
      text-decoration: none;
      transition: border-color 140ms ease, color 140ms ease, background 140ms ease;
    }
    .topnav a:hover {
      color: var(--ink);
      border-color: rgba(126, 231, 214, 0.4);
      text-decoration: none;
    }
    .topnav a.active {
      color: var(--ink);
      border-color: rgba(126, 231, 214, 0.48);
      background: linear-gradient(120deg, rgba(126, 231, 214, 0.16), rgba(126, 231, 214, 0.05));
      box-shadow: inset 0 0 0 1px rgba(126, 231, 214, 0.12);
    }
    .hero {
      display: grid;
      grid-template-columns: 1fr;
      gap: clamp(10px, 1.2vw, 16px);
      margin-bottom: clamp(10px, 1.2vw, 16px);
    }
    .hero-main, .card {
      background: linear-gradient(160deg, rgba(26, 24, 21, 0.92), rgba(14, 13, 12, 0.85));
      border: 1px solid rgba(255, 244, 230, 0.16);
      border-radius: 20px;
      box-shadow: var(--shadow-soft);
      backdrop-filter: blur(18px) saturate(120%);
      min-width: 0;
      transition: box-shadow 160ms ease, border-color 160ms ease, transform 160ms ease;
    }
    .hero-main:hover, .card:hover {
      border-color: rgba(126, 231, 214, 0.22);
      box-shadow: var(--shadow), var(--glow);
      transform: translateY(-1px);
    }
    .hero-main {
      display: grid;
      grid-template-columns: minmax(0, 1.1fr) minmax(320px, 0.9fr);
      gap: 14px;
      padding: 16px 18px;
      overflow: hidden;
      position: relative;
    }
    .hero-copy {
      min-width: 0;
      position: relative;
      z-index: 1;
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
      margin: 8px 0 0;
      font-size: clamp(26px, 4vw, 44px);
      line-height: 0.96;
      letter-spacing: -0.05em;
    }
    .sub {
      color: var(--muted);
      margin-top: 8px;
      max-width: 640px;
      font-size: 13px;
      line-height: 1.45;
    }
    .hero-meta {
      display: flex;
      flex-wrap: wrap;
      gap: 8px;
      margin-top: 12px;
    }
    .hero-side {
      position: relative;
      z-index: 1;
      display: grid;
      align-content: start;
      gap: 8px;
      min-width: 0;
    }
    .hero-priority-grid {
      display: grid;
      grid-template-columns: repeat(2, minmax(0, 1fr));
      gap: 8px;
    }
    .hero-priority-grid .surface-card {
      min-height: 94px;
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
    .layout {
      display: grid;
      grid-template-columns: repeat(12, 1fr);
      gap: clamp(10px, 1.1vw, 14px);
      margin-top: clamp(10px, 1.1vw, 14px);
      align-items: start;
      grid-auto-flow: row dense;
    }
    .card {
      grid-column: span 12;
      padding: 12px;
      overflow: hidden;
      align-self: start;
    }
    .compact-card {
      padding: 10px;
      align-self: start;
    }
    .span-7 { grid-column: span 7; }
    .span-5 { grid-column: span 5; }
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
    .metrics.three {
      grid-template-columns: repeat(3, minmax(0, 1fr));
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
    .compact-card .metric .v {
      font-size: 20px;
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
    .compact-card .section-head {
      margin-bottom: 10px;
    }
    .title {
      font-size: 17px;
      font-weight: 800;
      letter-spacing: -0.03em;
    }
    .compact-card .title {
      font-size: 15px;
    }
    .caption {
      color: var(--muted);
      font-size: 12px;
    }
    .compact-card .caption {
      font-size: 11px;
    }
    .section-meta-strip {
      display: flex;
      flex-wrap: wrap;
      gap: 8px;
      justify-content: flex-end;
      max-width: 100%;
    }
    .surface-strip {
      display: grid;
      grid-template-columns: repeat(3, minmax(0, 1fr));
      gap: 8px;
      margin-bottom: 10px;
    }
    #runtime-card .surface-strip {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }
    #costMetrics {
      grid-template-columns: repeat(3, minmax(0, 1fr));
    }
    .surface-card {
      border: 1px solid var(--line);
      border-radius: 16px;
      padding: 12px;
      background: linear-gradient(160deg, rgba(255,255,255,0.06), rgba(255,255,255,0.02));
      display: grid;
      gap: 6px;
      min-width: 0;
    }
    .surface-card-label {
      color: var(--muted);
      font-size: 10px;
      letter-spacing: 0.08em;
      text-transform: uppercase;
    }
    .surface-card-value {
      font-size: 18px;
      font-weight: 800;
      letter-spacing: -0.03em;
      line-height: 1.1;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }
    .compact-card .surface-card-value {
      font-size: 16px;
    }
    .surface-card-meta {
      color: var(--muted);
      font-size: 11px;
      line-height: 1.45;
      overflow-wrap: anywhere;
    }
    .surface-card.tone-good {
      border-color: rgba(126, 231, 214, 0.28);
      background: linear-gradient(160deg, rgba(126, 231, 214, 0.12), rgba(255,255,255,0.02));
    }
    .surface-card.tone-warn {
      border-color: rgba(241, 184, 102, 0.28);
      background: linear-gradient(160deg, rgba(241, 184, 102, 0.12), rgba(255,255,255,0.02));
    }
    .surface-card.tone-danger {
      border-color: rgba(255, 127, 110, 0.30);
      background: linear-gradient(160deg, rgba(255, 127, 110, 0.12), rgba(255,255,255,0.02));
    }
    .mini-chip {
      display: inline-flex;
      align-items: center;
      gap: 6px;
      padding: 7px 10px;
      border-radius: 999px;
      border: 1px solid var(--line);
      background: rgba(255,255,255,0.04);
      color: var(--muted);
      font-size: 11px;
      line-height: 1.2;
      white-space: nowrap;
    }
    .mini-chip strong {
      color: var(--ink);
      font-size: 11px;
      letter-spacing: 0.04em;
      text-transform: uppercase;
    }
    .mini-chip.accent {
      border-color: rgba(126, 231, 214, 0.34);
      color: var(--accent);
      background: rgba(126, 231, 214, 0.09);
    }
    .mini-chip.warn {
      border-color: rgba(241, 184, 102, 0.34);
      color: var(--amber);
      background: rgba(241, 184, 102, 0.09);
    }
    .mini-chip.danger {
      border-color: rgba(255, 127, 110, 0.34);
      color: var(--danger);
      background: rgba(255, 127, 110, 0.09);
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
    .page-settings .hero {
      grid-template-columns: 1fr;
    }
    .page-settings .hero-main {
      grid-template-columns: 1fr;
      min-height: 0;
    }
    .page-settings .hero-side {
      display: none;
    }
    a { color: var(--accent); text-decoration: none; }
    a:hover { text-decoration: underline; }
    .table-requests table { min-width: 1120px; }
    .table-models table { min-width: 720px; }
    .table-health table { min-width: 900px; }
    .table-usage table { min-width: 520px; }
    .table-cache table { min-width: 580px; }
    .upstream-grid {
      display: grid;
      grid-template-columns: repeat(2, minmax(0, 1fr));
      gap: 10px;
    }
    .upstream-tile {
      border: 1px solid var(--line);
      border-radius: 18px;
      background: linear-gradient(160deg, rgba(255,255,255,0.06), rgba(255,255,255,0.02));
      padding: 12px;
      display: grid;
      gap: 10px;
      min-width: 0;
    }
    .upstream-tile.is-degraded {
      border-color: rgba(255, 127, 110, 0.30);
      background: linear-gradient(160deg, rgba(255, 127, 110, 0.12), rgba(255,255,255,0.02));
    }
    .upstream-tile.is-warn {
      border-color: rgba(241, 184, 102, 0.30);
      background: linear-gradient(160deg, rgba(241, 184, 102, 0.12), rgba(255,255,255,0.02));
    }
    .upstream-head {
      display: flex;
      align-items: flex-start;
      justify-content: space-between;
      gap: 10px;
      flex-wrap: wrap;
    }
    .upstream-heading {
      display: grid;
      gap: 4px;
      min-width: 0;
    }
    .upstream-name {
      font-size: 15px;
      font-weight: 800;
      letter-spacing: -0.02em;
      overflow-wrap: anywhere;
      word-break: break-word;
    }
    .upstream-note {
      color: var(--muted);
      font-size: 11px;
      line-height: 1.4;
      overflow-wrap: anywhere;
      word-break: break-word;
    }
    .upstream-stats {
      display: grid;
      grid-template-columns: repeat(3, minmax(0, 1fr));
      gap: 8px;
    }
    .upstream-stat {
      border: 1px solid rgba(255, 244, 230, 0.10);
      border-radius: 14px;
      padding: 10px;
      background: rgba(255,255,255,0.03);
      display: grid;
      gap: 4px;
      min-width: 0;
    }
    .upstream-stat-label {
      color: var(--muted);
      font-size: 10px;
      letter-spacing: 0.08em;
      text-transform: uppercase;
    }
    .upstream-stat-value {
      font-size: 16px;
      font-weight: 800;
      letter-spacing: -0.03em;
      line-height: 1.1;
      overflow-wrap: anywhere;
      word-break: break-word;
    }
    .error-feed {
      display: grid;
      gap: 10px;
    }
    .error-item {
      border: 1px solid var(--line);
      border-radius: 18px;
      background: linear-gradient(160deg, rgba(255,255,255,0.06), rgba(255,255,255,0.02));
      padding: 12px;
      display: grid;
      gap: 8px;
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
      font-size: 15px;
      font-weight: 800;
      letter-spacing: -0.02em;
    }
    .error-message {
      margin-top: 4px;
      line-height: 1.5;
      overflow-wrap: anywhere;
      word-break: break-word;
      display: -webkit-box;
      -webkit-line-clamp: 4;
      -webkit-box-orient: vertical;
      overflow: hidden;
    }
    .error-meta {
      display: flex;
      flex-wrap: wrap;
      gap: 8px;
    }
    .error-frame {
      display: grid;
      gap: 8px;
    }
    .error-context {
      display: grid;
      gap: 4px;
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
    .settings-shell {
      display: grid;
      grid-template-columns: 220px minmax(0, 1fr) 340px;
      gap: 14px;
      align-items: start;
    }
    .settings-nav,
    .settings-main,
    .settings-rail {
      display: grid;
      gap: 12px;
      min-width: 0;
    }
    .settings-sticky {
      position: sticky;
      top: 14px;
      display: grid;
      gap: 12px;
    }
    .settings-rail-panel {
      padding: 12px;
    }
    .settings-nav-panel {
      padding: 12px;
    }
    .settings-nav-title {
      font-size: 11px;
      letter-spacing: 0.08em;
      text-transform: uppercase;
      color: var(--muted);
      margin-bottom: 8px;
    }
    .settings-jumpbar {
      display: grid;
      gap: 8px;
      margin-bottom: 2px;
    }
    .settings-jumpbar a {
      display: grid;
      grid-template-columns: minmax(0, 1fr) minmax(64px, 92px);
      align-items: start;
      gap: 10px;
      padding: 10px 12px;
      border-radius: 14px;
      border: 1px solid var(--line);
      background: rgba(255,255,255,0.04);
      color: var(--muted);
      font-size: 12px;
      text-decoration: none;
      min-width: 0;
      overflow: hidden;
    }
    .settings-jumpbar a:hover {
      color: var(--ink);
      border-color: rgba(126, 231, 214, 0.4);
      text-decoration: none;
    }
    .settings-jumpbar a.active {
      color: var(--ink);
      border-color: rgba(126, 231, 214, 0.48);
      background: linear-gradient(120deg, rgba(126, 231, 214, 0.16), rgba(126, 231, 214, 0.04));
      box-shadow: inset 0 0 0 1px rgba(126, 231, 214, 0.12);
    }
    .settings-jumpbar-copy {
      display: grid;
      gap: 3px;
      min-width: 0;
    }
    .settings-jumpbar strong {
      font-size: 12px;
      letter-spacing: 0.04em;
      text-transform: uppercase;
      white-space: nowrap;
    }
    .settings-jumpbar span {
      color: var(--muted);
      font-size: 11px;
    }
    .settings-jumpbar em {
      font-style: normal;
      font-size: 10px;
      letter-spacing: 0.08em;
      text-transform: uppercase;
      color: rgba(247, 243, 238, 0.72);
      white-space: normal;
      line-height: 1.3;
      text-align: right;
      overflow-wrap: anywhere;
      word-break: break-word;
      min-width: 0;
    }
    .settings-jumpbar em.meta-good {
      color: var(--accent);
    }
    .settings-jumpbar em.meta-warn {
      color: var(--amber);
    }
    .settings-jumpbar em.meta-danger {
      color: var(--danger);
    }
    .config-grid {
      display: grid;
      grid-template-columns: repeat(2, minmax(0, 1fr));
      gap: 10px;
    }
    .page-settings .config-grid {
      grid-template-columns: repeat(3, minmax(0, 1fr));
      gap: 8px;
    }
    .policy-grid {
      display: grid;
      grid-template-columns: repeat(3, minmax(0, 1fr));
      gap: 8px;
      margin-bottom: 8px;
    }
    .policy-card {
      border: 1px solid var(--line);
      border-radius: 14px;
      padding: 12px;
      background: linear-gradient(160deg, rgba(255,255,255,0.05), rgba(255,255,255,0.02));
      display: grid;
      gap: 5px;
      min-width: 0;
      transition: border-color 120ms ease, box-shadow 120ms ease, transform 120ms ease;
    }
    .policy-card.active {
      border-color: rgba(126, 231, 214, 0.42);
      box-shadow: inset 0 0 0 1px rgba(126, 231, 214, 0.12), 0 14px 26px rgba(121, 230, 215, 0.08);
      transform: translateY(-1px);
    }
    .policy-card.warn {
      border-color: rgba(241, 184, 102, 0.32);
      box-shadow: inset 0 0 0 1px rgba(241, 184, 102, 0.1);
    }
    .policy-card-label {
      color: var(--muted);
      font-size: 10px;
      letter-spacing: 0.1em;
      text-transform: uppercase;
    }
    .policy-card-value {
      font-size: 20px;
      font-weight: 800;
      letter-spacing: -0.04em;
      line-height: 1;
    }
    .policy-card-meta {
      color: var(--muted);
      font-size: 11px;
      line-height: 1.4;
    }
    .mode-preset-grid {
      display: grid;
      grid-template-columns: repeat(2, minmax(0, 1fr));
      gap: 8px;
      margin-bottom: 10px;
    }
    .mode-preset {
      width: 100%;
      color: var(--ink);
      text-align: left;
      cursor: pointer;
      appearance: none;
    }
    .mode-preset:hover {
      border-color: rgba(126, 231, 214, 0.38);
      transform: translateY(-1px);
    }
    .mode-preset.active {
      border-color: rgba(126, 231, 214, 0.44);
      box-shadow: inset 0 0 0 1px rgba(126, 231, 214, 0.14), 0 16px 32px rgba(121, 230, 215, 0.08);
    }
    .config-field {
      display: flex;
      flex-direction: column;
      gap: 5px;
    }
    .config-field.subdued {
      opacity: 0.58;
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
    .config-section {
      position: relative;
      overflow: clip;
    }
    .config-section::before {
      content: "";
      position: absolute;
      inset: 0 auto 0 0;
      width: 3px;
      border-radius: 999px;
      background: linear-gradient(180deg, rgba(126, 231, 214, 0.9), rgba(241, 184, 102, 0.3));
      opacity: 0.72;
    }
    .config-section.retry-focus {
      border-color: rgba(126, 231, 214, 0.24);
      background:
        radial-gradient(circle at 100% 0%, rgba(126, 231, 214, 0.10), transparent 42%),
        linear-gradient(160deg, rgba(255,255,255,0.06), rgba(255,255,255,0.02));
    }
    .config-card-head {
      display: flex;
      justify-content: space-between;
      align-items: flex-start;
      gap: 6px;
    }
    .page-settings .config-card-head {
      position: sticky;
      top: 10px;
      z-index: 2;
      margin: -2px -2px 8px;
      padding: 2px 2px 8px;
      flex-wrap: wrap;
      background: linear-gradient(180deg, rgba(14, 13, 12, 0.98), rgba(14, 13, 12, 0.9) 82%, rgba(14, 13, 12, 0));
    }
    .config-card-title {
      font-weight: 700;
    }
    .config-card-head-main {
      display: grid;
      gap: 4px;
      min-width: 0;
    }
    .section-kicker {
      display: inline-flex;
      align-items: center;
      gap: 8px;
      color: var(--muted);
      font-size: 10px;
      letter-spacing: 0.14em;
      text-transform: uppercase;
    }
    .section-kicker strong {
      color: var(--accent-strong);
      font-size: 11px;
      letter-spacing: 0.18em;
    }
    .section-kicker span {
      padding-top: 1px;
    }
    .section-inline-meta {
      display: flex;
      flex-wrap: wrap;
      gap: 6px;
      margin-top: 2px;
      min-width: 0;
    }
    .section-inline-meta .mini-chip {
      white-space: normal;
      overflow-wrap: anywhere;
      word-break: break-word;
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
    .config-filter {
      min-width: 180px;
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
    .provider-card.collapsed > :not(.config-card-head):not(.provider-summary-strip):not(.probe-status-host) {
      display: none;
    }
    .provider-card.hidden-provider {
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
    .settings-rail-panel.command-panel {
      background:
        radial-gradient(circle at 0% 0%, rgba(241, 184, 102, 0.08), transparent 42%),
        linear-gradient(160deg, rgba(255,255,255,0.06), rgba(255,255,255,0.02));
    }
    .settings-rail-actions {
      display: grid;
      grid-template-columns: repeat(2, minmax(0, 1fr));
      gap: 8px;
      margin-top: 2px;
    }
    .settings-rail-actions .btn {
      justify-content: center;
    }
    .settings-rail-actions .config-hint {
      grid-column: 1 / -1;
      min-height: 0;
      padding: 2px 2px 0;
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
    .provider-summary-strip {
      display: grid;
      grid-template-columns: 1.1fr 1.55fr 0.65fr 0.65fr 0.8fr 0.9fr 1.15fr;
      gap: 8px;
      margin: 2px 0 4px;
      padding: 8px 10px;
      border-radius: 14px;
      border: 1px solid var(--line);
      background: rgba(255,255,255,0.03);
    }
    .provider-summary-item {
      display: grid;
      gap: 4px;
      min-width: 0;
    }
    .provider-summary-label {
      color: var(--muted);
      font-size: 10px;
      letter-spacing: 0.08em;
      text-transform: uppercase;
    }
    .provider-summary-value {
      font-size: 12px;
      color: var(--ink);
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }
    .provider-summary-value.probe-ok {
      color: var(--accent);
    }
    .provider-summary-value.probe-fail {
      color: var(--danger);
    }
    .provider-summary-value.probe-idle {
      color: var(--muted);
    }
    .provider-chip {
      display: inline-flex;
      align-items: center;
      gap: 6px;
      padding: 5px 9px;
      border-radius: 999px;
      border: 1px solid var(--line);
      background: rgba(255,255,255,0.04);
      color: var(--muted);
      font-size: 11px;
      line-height: 1.2;
    }
    .provider-chip.accent {
      border-color: rgba(121, 230, 215, 0.24);
      color: var(--accent);
      background: rgba(121, 230, 215, 0.08);
    }
    .provider-chip.warn {
      border-color: rgba(241, 184, 102, 0.28);
      color: var(--amber);
      background: rgba(241, 184, 102, 0.08);
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
    .config-hint.is-dirty {
      color: var(--amber);
    }
    .config-hint.is-saved {
      color: var(--accent);
    }
    .history-name {
      font-weight: 700;
      overflow-wrap: anywhere;
      word-break: break-word;
    }
    .card-fill {
      display: flex;
      flex-direction: column;
      min-height: 0;
    }
    .card-fill.priority-feed {
      min-height: 0;
    }
    .card-fill.compact-feed {
      min-height: 0;
    }
    .card-fill-body {
      flex: 0 1 auto;
      min-height: 0;
      max-height: clamp(220px, 34vh, 420px);
      overflow: auto;
    }
    .priority-feed .card-fill-body {
      max-height: clamp(260px, 42vh, 540px);
    }
    .compact-feed .card-fill-body {
      max-height: clamp(180px, 28vh, 300px);
    }
    .chart-wrap {
      position: relative;
      width: 100%;
      height: 190px;
      overflow: hidden;
    }
    .compact-chart .chart-wrap {
      height: 160px;
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
    #performance,
    #economics,
    #upstreams-card,
    #requests-card {
      scroll-margin-top: 88px;
    }
    .config-section {
      scroll-margin-top: 18px;
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
    .cell-stack {
      display: grid;
      gap: 4px;
      min-width: 0;
    }
    .cell-main {
      color: var(--ink);
      font-size: 12px;
      line-height: 1.35;
      overflow-wrap: anywhere;
      word-break: break-word;
    }
    .cell-sub {
      color: var(--muted);
      font-size: 11px;
      line-height: 1.35;
      overflow-wrap: anywhere;
      word-break: break-word;
    }
    .cell-tags {
      display: flex;
      flex-wrap: wrap;
      gap: 6px;
    }
    .status-chip {
      display: inline-flex;
      align-items: center;
      justify-content: center;
      min-width: 54px;
      padding: 5px 10px;
      border-radius: 999px;
      border: 1px solid var(--line);
      background: rgba(255,255,255,0.05);
      color: var(--muted);
      font-size: 11px;
      font-weight: 700;
      letter-spacing: 0.04em;
    }
    .status-chip.ok {
      color: var(--accent);
      border-color: rgba(121, 230, 215, 0.32);
      background: rgba(121, 230, 215, 0.08);
    }
    .status-chip.warn {
      color: var(--amber);
      border-color: rgba(241, 184, 102, 0.32);
      background: rgba(241, 184, 102, 0.08);
    }
    .status-chip.danger {
      color: var(--danger);
      border-color: rgba(255, 127, 110, 0.36);
      background: rgba(255, 127, 110, 0.10);
    }
    @media (max-width: 900px) {
      .hero {
        grid-template-columns: 1fr;
      }
      .hero-main {
        grid-template-columns: 1fr;
      }
      .hero-priority-grid {
        grid-template-columns: 1fr;
      }
      .span-8, .span-7, .span-6, .span-5, .span-4 {
        grid-column: span 12;
      }
      .metrics, .page-settings .config-grid, .policy-grid, .provider-summary-strip, .surface-strip {
        grid-template-columns: repeat(2, 1fr);
      }
      .mode-preset-grid {
        grid-template-columns: 1fr;
      }
      .settings-rail-actions {
        grid-template-columns: 1fr;
      }
      .upstream-grid {
        grid-template-columns: 1fr;
      }
    }
    @media (max-width: 640px) {
      .metrics, .page-settings .config-grid, .policy-grid, .provider-summary-strip, .surface-strip {
        grid-template-columns: 1fr;
      }
      .mode-preset-grid {
        grid-template-columns: 1fr;
      }
      .upstream-stats {
        grid-template-columns: 1fr;
      }
    }
    @media (max-width: 1320px) {
      .page-settings .config-grid,
      .policy-grid {
        grid-template-columns: repeat(2, minmax(0, 1fr));
      }
      .settings-jumpbar a {
        grid-template-columns: 1fr;
      }
      .settings-jumpbar em {
        text-align: left;
        letter-spacing: 0.06em;
      }
    }
    @media (max-width: 1380px) {
      .settings-shell {
        grid-template-columns: 220px minmax(0, 1fr);
      }
      .settings-rail {
        grid-column: 1 / -1;
      }
      .settings-rail .settings-sticky {
        position: static;
      }
    }
    @media (max-width: 1180px) {
      .span-4, .span-5, .span-7, .span-8 {
        grid-column: span 12;
      }
      .page-settings .config-grid,
      .policy-grid,
      .surface-strip {
        grid-template-columns: 1fr;
      }
      .mode-preset-grid {
        grid-template-columns: 1fr;
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
      .settings-shell {
        grid-template-columns: 1fr;
      }
      .settings-sticky {
        position: static;
      }
    }
    @media (min-width: 1600px) {
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
        {{TOPNAV_LINKS}}
      </div>
    </div>
    <div class="hero">
      <div class="hero-main">
        <div class="hero-copy">
          <div class="hero-head">
            <div>
              <div class="eyebrow">{{HERO_EYEBROW}}</div>
              <h1>{{HERO_TITLE}}</h1>
            </div>
          </div>
          <div class="sub">{{HERO_SUB}}</div>
          <div class="hero-meta">
            {{HERO_META_PRIMARY}}
            {{HERO_META_SECONDARY}}
            {{HERO_META_TERTIARY}}
          </div>
        </div>
        <div class="hero-side">
          {{HERO_ASIDE}}
        </div>
      </div>
    </div>

    <div id="overviewShell">
    <div class="layout overview-primary">
      <div class="card span-8" id="performance">
        <div class="section-head">
          <div>
            <div class="title">Live Performance</div>
            <div class="caption">先看最近 1 分钟与 5 分钟窗口内的真实 RPM、TPM 和延迟。</div>
          </div>
          <div class="section-meta-strip" id="performanceMeta"></div>
        </div>
        <div class="metrics" id="metrics"></div>
      </div>
      <div class="card span-4 compact-card" id="runtime-card">
        <div class="section-head">
          <div>
            <div class="title">Runtime Posture</div>
            <div class="caption">当前恢复模式、失败出口和探活状态。</div>
          </div>
        </div>
        <div class="surface-strip" id="runtimeTopline"></div>
        <div class="metrics three" id="runtimeMetrics"></div>
      </div>
      <div class="card span-7" id="upstreams-card">
        <div class="section-head">
          <div>
            <div class="title">Upstream Health</div>
            <div class="caption">先看哪条上游慢、退化、进冷却，再决定是否切换。</div>
          </div>
          <div class="section-meta-strip" id="upstreamMeta"></div>
        </div>
        <div class="surface-strip" id="upstreamTopline"></div>
        <div id="upstreams"></div>
      </div>
      <div class="card span-5 card-fill compact-card compact-feed" id="errors-card">
        <div class="section-head">
          <div>
            <div class="title">Recent Errors</div>
            <div class="caption">压缩看最近错误的归属、状态和主消息。</div>
          </div>
          <div class="section-meta-strip" id="errorsMeta"></div>
        </div>
        <div class="surface-strip" id="errorsTopline"></div>
        <div class="card-fill-body" id="errors"></div>
      </div>
      <div class="card span-12 card-fill priority-feed" id="requests-card">
        <div class="section-head">
          <div>
            <div class="title">Recent Requests</div>
            <div class="caption">最新请求轨迹，含状态、尝试次数、延迟和单次估算成本。</div>
          </div>
          <div class="section-meta-strip" id="requestsMeta"></div>
        </div>
        <div class="surface-strip" id="requestsTopline"></div>
        <div class="card-fill-body" id="requests"></div>
      </div>
    </div>

    <div class="layout overview-charts" id="chartLayout">
      <div class="card span-7">
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
      <div class="card span-5">
        <div class="section-head">
          <div>
            <div class="title">Latency Trend</div>
            <div class="caption">平均延迟（ms）与成功率趋势。</div>
          </div>
        </div>
        <div class="chart-wrap" id="chartLatency"><div class="chart-tooltip" id="tipLatency"></div></div>
        <div class="chart-legend" id="legendLatency"></div>
      </div>
      <div class="card span-7">
        <div class="section-head">
          <div>
            <div class="title">Success / Failure</div>
            <div class="caption">每个时间桶内的成功与失败请求数。</div>
          </div>
        </div>
        <div class="chart-wrap" id="chartSuccess"><div class="chart-tooltip" id="tipSuccess"></div></div>
        <div class="chart-legend" id="legendSuccess"></div>
      </div>
      <div class="card span-5 compact-card compact-chart">
        <div class="section-head">
          <div>
            <div class="title">Token Usage by Upstream</div>
            <div class="caption">按上游分组的 token 消耗。</div>
          </div>
        </div>
        <div class="chart-wrap" id="chartTokens"><div class="chart-tooltip" id="tipTokens"></div></div>
        <div class="chart-legend" id="legendTokens"></div>
      </div>
    </div>

    <div class="layout overview-economics">
      <div class="card span-8" id="economics">
        <div class="section-head">
          <div>
            <div class="title">Model Economics</div>
            <div class="caption">按模型汇总 token、估算美元成本和官方价格表覆盖情况。</div>
          </div>
          <div class="section-meta-strip" id="economicsMeta"></div>
        </div>
        <div class="surface-strip" id="economicsTopline"></div>
        <div id="byModel"></div>
      </div>
      <div class="card span-4 compact-card" id="cost-card">
        <div class="section-head">
          <div>
            <div class="title">Cost Snapshot</div>
            <div class="caption">基于已知官方价格模型估算。</div>
          </div>
          <div class="section-meta-strip" id="costMeta"></div>
        </div>
        <div class="metrics" id="costMetrics"></div>
      </div>
      <div class="card span-8 compact-card" id="usage-card">
        <div class="section-head">
          <div>
            <div class="title">Upstream Usage</div>
            <div class="caption">按上游汇总 token 消耗，和健康状态对照看。</div>
          </div>
          <div class="section-meta-strip" id="usageMeta"></div>
        </div>
        <div class="surface-strip" id="usageTopline"></div>
        <div id="byUpstream"></div>
      </div>
      <div class="card span-4 compact-card" id="cache-card">
        <div class="section-head">
          <div>
            <div class="title">Cache Hit Ranking</div>
            <div class="caption">最近 24 小时按上游缓存命中率排序。</div>
          </div>
          <div class="section-meta-strip" id="cacheMeta"></div>
        </div>
        <div class="surface-strip" id="cacheTopline"></div>
        <div id="cacheRanking"></div>
      </div>
    </div>
    </div>
    <div class="card span-12 is-hidden" id="runtimeConfig">
        <div class="section-head">
          <div>
            <div class="title">Runtime Config</div>
            <div class="caption">集中编辑探活、桥接、恢复和服务商配置。</div>
          </div>
          <div class="config-status" id="configStatus">加载中</div>
        </div>
        <div class="config-panel">
          <div class="settings-shell" id="settingsShell">
            <aside class="settings-nav">
              <div class="settings-sticky">
                <div class="config-card settings-nav-panel" id="settingsNav">
                  <div class="settings-nav-title">Sections</div>
                  <div class="settings-jumpbar">
                    <a href="#cfg-health" data-nav-target="cfg-health"><div class="settings-jumpbar-copy"><strong>Health</strong></div><em id="navMetaHealth">path</em></a>
                    <a href="#cfg-bridge" data-nav-target="cfg-bridge"><div class="settings-jumpbar-copy"><strong>Bridge</strong></div><em id="navMetaBridge">0 rules</em></a>
                    <a href="#cfg-router" data-nav-target="cfg-router"><div class="settings-jumpbar-copy"><strong>Router</strong></div><em id="navMetaRouter">strategy</em></a>
                    <a href="#cfg-intercepts" data-nav-target="cfg-intercepts"><div class="settings-jumpbar-copy"><strong>Intercepts</strong></div><em id="navMetaIntercepts">0 rules</em></a>
                    <a href="#cfg-upstreams" data-nav-target="cfg-upstreams"><div class="settings-jumpbar-copy"><strong>Providers</strong></div><em id="navMetaProviders">0 providers</em></a>
                    <a href="#cfg-history" data-nav-target="cfg-history"><div class="settings-jumpbar-copy"><strong>History</strong></div><em id="navMetaHistory">0 versions</em></a>
                  </div>
                </div>
              </div>
            </aside>
            <div class="settings-main">
          <div class="config-card config-section" id="cfg-health" data-section-title="Health Check">
            <div class="config-card-head">
              <div class="config-card-head-main">
                <div class="section-kicker"><strong>01</strong><span>Runtime Guardrail</span></div>
                <div class="config-card-title">Health Check</div>
                <div class="config-help">控制主动探活的开关、间隔、超时和路径</div>
                <div class="section-inline-meta" id="cfgHealthMeta"></div>
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
                <div class="section-kicker"><strong>02</strong><span>Rewrite Surface</span></div>
                <div class="config-card-title">Model Bridge</div>
                <div class="config-help">维护模型别名映射和需跳过桥接的 User-Agent</div>
                <div class="section-inline-meta" id="cfgBridgeMeta"></div>
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
              <button class="btn secondary" id="applyCodexBridgePreset" type="button">Apply GPT-5.x Bridge Preset</button>
              <button class="btn secondary" id="addBridgeRule">Add Bridge Rule</button>
            </div>
          </div>
          <div class="config-card config-section" id="cfg-router" data-section-title="Router Retry">
            <div class="config-card-head">
              <div class="config-card-head-main">
                <div class="section-kicker"><strong>03</strong><span>Traffic Strategy</span></div>
                <div class="config-card-title">Router Retry</div>
                <div class="config-help">控制重试次数与退避窗口</div>
                <div class="section-inline-meta" id="cfgRouterMeta"></div>
              </div>
            </div>
            <div class="config-grid">
              <div class="config-field">
                <label>Router Strategy</label>
                <select id="cfgRouterStrategy">
                  <option value="health_weighted_rr">health_weighted_rr</option>
                  <option value="round_robin">round_robin</option>
                </select>
              </div>
              <div class="config-field">
                <label>Max Retries</label>
                <input type="number" min="0" id="cfgMaxRetries" />
                <div class="config-help" id="cfgMaxRetriesHint">Maximum retry rounds before the gateway stops retrying.</div>
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
                <div class="config-help" id="cfgPassthroughHint">After this window, retryable failures can surface the upstream response.</div>
              </div>
            </div>
          </div>
          <div class="config-card config-section retry-focus" data-section-title="Retry Policy">
            <div class="config-card-head">
              <div class="config-card-head-main">
                <div class="section-kicker"><strong>04</strong><span>Recovery Policy</span></div>
                <div class="config-card-title">Retry Policy</div>
                <div class="config-help">命中状态码或关键字后触发重试，也可以切到“任何错误都无限重试”的恢复模式</div>
                <div class="section-inline-meta" id="cfgRetryMeta"></div>
              </div>
            </div>
            <div class="policy-grid">
              <div class="policy-card" id="retryModeCard">
                <div class="policy-card-label">Recovery Mode</div>
                <div class="policy-card-value mono" id="retryModeValue">bounded</div>
                <div class="policy-card-meta" id="retryModeMeta">Retry only matched retryable failures.</div>
              </div>
              <div class="policy-card" id="retryBackoffCard">
                <div class="policy-card-label">Backoff Window</div>
                <div class="policy-card-value mono" id="retryBackoffValue">0 ms</div>
                <div class="policy-card-meta" id="retryBackoffMeta">Base and cap for exponential backoff.</div>
              </div>
              <div class="policy-card" id="retryPassthroughCard">
                <div class="policy-card-label">Failure Exit</div>
                <div class="policy-card-value mono" id="retryPassthroughValue">bounded</div>
                <div class="policy-card-meta" id="retryPassthroughMeta">After the retry window, the gateway can surface the upstream failure.</div>
              </div>
            </div>
            <div class="mode-preset-grid">
              <button class="policy-card mode-preset" id="retryModePresetBounded" type="button">
                <div class="policy-card-label">Preset</div>
                <div class="policy-card-value">Bounded Failover</div>
                <div class="policy-card-meta">Respect max retries and allow passthrough after the configured failure window.</div>
              </button>
              <button class="policy-card mode-preset" id="retryModePresetInfinite" type="button">
                <div class="policy-card-label">Preset</div>
                <div class="policy-card-value">Infinite Recovery</div>
                <div class="policy-card-meta">Retry transport, status, and intercepted body errors until the caller cancels.</div>
              </button>
            </div>
            <div class="config-grid">
              <div class="config-field">
                <label>Recovery Mode</label>
                <label class="small"><input type="checkbox" id="cfgRetryInfiniteOnError"> Infinite retry on any error</label>
                <div class="config-help">Transport errors, response status errors, and intercepted body errors keep retrying until the client cancels.</div>
              </div>
              <div class="config-field">
                <label>Status Codes</label>
                <input type="text" id="cfgRetryCodes" placeholder="408,429,500,502,503,504" />
              </div>
              <div class="config-field">
                <label>Status Code Min</label>
                <input type="number" min="0" id="cfgRetryMin" placeholder="500" />
              </div>
              <div class="config-field" style="grid-column: span 3;">
                <label>Message Keywords</label>
                <textarea id="cfgRetryKeywords" placeholder="rate limit\nupstream request failed"></textarea>
              </div>
            </div>
          </div>
          <div class="config-card config-section" id="cfg-intercepts" data-section-title="Response Intercepts">
            <div class="config-card-head">
              <div class="config-card-head-main">
                <div class="section-kicker"><strong>05</strong><span>Response Traps</span></div>
                <div class="config-card-title">Response Intercepts</div>
                <div class="config-help">按路径、状态码或错误关键字提前判定 retry / fail</div>
                <div class="section-inline-meta" id="cfgInterceptMeta"></div>
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
                <div class="section-kicker"><strong>06</strong><span>Provider Matrix</span></div>
                <div class="config-card-title">Service Providers</div>
                <div class="config-help">维护上游服务商的 URL、API key、模型范围和超时；每一项都可先行测试再保存</div>
                <div class="section-inline-meta" id="cfgUpstreamsMeta"></div>
              </div>
            </div>
            <div id="upstreamConfigList"></div>
            <div class="config-actions">
              <button class="btn secondary" id="addUpstream">Add Provider</button>
            </div>
          </div>
            </div>
            <aside class="settings-rail">
              <div class="settings-sticky">
                <div class="config-card settings-rail-panel command-panel">
                    <div class="config-card-head">
                      <div class="config-card-head-main">
                        <div class="section-kicker"><strong>Ops</strong><span>Control Deck</span></div>
                        <div class="config-card-title">Controls</div>
                      </div>
                  </div>
                  <div class="config-toolbar">
                    <input class="config-search" id="configSearch" type="search" placeholder="Search sections or providers..." />
                    <select class="config-filter" id="providerClassFilter">
                      <option value="all">All Providers</option>
                      <option value="free">Free First</option>
                      <option value="quota_limited">Quota-Limited</option>
                    </select>
                    <select class="config-filter" id="cfgAdminLanguage">
                      <option value="zh">中文</option>
                      <option value="en">English</option>
                    </select>
                    <button class="btn secondary" id="expandSections" type="button">Expand All</button>
                    <button class="btn secondary" id="collapseSections" type="button">Collapse All</button>
                  </div>
                  <div class="validation-summary" id="configValidation"></div>
                  <div class="config-actions settings-rail-actions">
                    <button class="btn" id="saveConfig">Save Config</button>
                    <button class="btn secondary" id="reloadConfig">Reload</button>
                    <button class="btn secondary" id="exportConfig">Export</button>
                    <button class="btn danger" id="rollbackConfig">Rollback</button>
                    <span class="config-hint" id="configHint"></span>
                  </div>
                </div>
                <div class="config-card settings-rail-panel" id="cfg-history">
                  <div class="config-card-head">
                    <div class="config-card-head-main">
                      <div class="config-card-title">History</div>
                    </div>
                  </div>
                  <div id="configHistoryList"></div>
                  <div id="configDiffPreview"></div>
                </div>
              </div>
            </aside>
          </div>
        </div>
      </div>
  </div>
  <script>
    let currentLocale = "{{BOOTSTRAP_LANGUAGE}}";
    const I18N = {
      zh: {
        brandTitle: "AI MODEL GATEWAY",
        brandSubtitle: "统一的可观测性、路由、计价和运行控制台。",
        overviewNavPerformance: "性能",
        overviewNavEconomics: "成本",
        overviewNavUpstreams: "上游",
        overviewNavRequests: "请求",
        overviewNavSettings: "设置",
        settingsNavOverview: "总览",
        performanceTitle: "实时性能",
        performanceCaption: "先看最近 1 分钟与 5 分钟窗口内的真实 RPM、TPM 和延迟。",
        runtimePostureTitle: "运行姿态",
        runtimePostureCaption: "当前恢复模式、失败出口和探活状态。",
        upstreamHealthTitle: "上游健康",
        upstreamHealthCaption: "先看哪条上游慢、退化、进冷却，再决定是否切换。",
        recentErrorsTitle: "最近错误",
        recentErrorsCaption: "压缩看最近错误的归属、状态和主消息。",
        recentRequestsTitle: "最近请求",
        recentRequestsCaption: "最新请求轨迹，含状态、尝试次数、延迟和单次估算成本。",
        requestThroughputTitle: "请求吞吐",
        requestThroughputCaption: "RPM 与 TPM 趋势，按时间桶聚合。",
        latencyTrendTitle: "延迟趋势",
        latencyTrendCaption: "平均延迟（ms）与成功率趋势。",
        successFailureTitle: "成功 / 失败",
        successFailureCaption: "每个时间桶内的成功与失败请求数。",
        tokenUsageTitle: "按上游 Token 用量",
        tokenUsageCaption: "按上游分组的 token 消耗。",
        economicsTitle: "模型成本",
        economicsCaption: "按模型汇总 token、估算美元成本和官方价格表覆盖情况。",
        costSnapshotTitle: "成本快照",
        costSnapshotCaption: "基于已知官方价格模型估算。",
        upstreamUsageTitle: "上游用量",
        upstreamUsageCaption: "按上游汇总 token 消耗，和健康状态对照看。",
        cacheRankingTitle: "缓存命中排行",
        cacheRankingCaption: "最近 24 小时按上游缓存命中率排序。",
        runtimeConfigTitle: "运行时配置",
        runtimeConfigCaption: "集中编辑探活、桥接、恢复、语言和服务商配置。",
        settingsNavTitle: "区块",
        settingsHealth: "探活",
        settingsBridge: "桥接",
        settingsRouter: "路由",
        settingsIntercepts: "拦截",
        settingsProviders: "服务商",
        settingsHistory: "历史",
        settingsControlsTitle: "控制",
        settingsHistoryTitle: "历史版本",
        settingsSearchPlaceholder: "搜索区块或服务商...",
        settingsExpandAll: "全部展开",
        settingsCollapseAll: "全部折叠",
        saveConfig: "保存配置",
        reloadConfig: "重新载入",
        exportConfig: "导出",
        rollbackConfig: "回滚",
        allProviders: "全部服务商",
        freeFirst: "免费优先",
        quotaLimited: "额度受限",
        languageLabel: "界面语言",
        languageChinese: "中文",
        languageEnglish: "English",
        loading: "加载中",
        loaded: "已载入",
        configSynced: "配置已同步",
        loadConfigFailed: "配置载入失败",
        loadingDiff: "加载差异中...",
        diffLoaded: "差异已载入",
        loadDiffFailed: "差异载入失败",
        testingProvider: "正在测试服务商...",
        providerTestPassed: "服务商测试通过",
        providerTestFailed: "服务商测试失败",
        fixValidationErrors: "请先修复校验错误再保存",
        saving: "保存中...",
        saved: "已保存",
        saveFailed: "保存失败",
        rollbackLatestConfirm: "回滚到最近一次保存的配置版本？",
        rollbackSelectedConfirm: "回滚到选中的配置版本？",
        rollingBack: "回滚中...",
        rolledBack: "已回滚",
        rollbackFailed: "回滚失败",
        appliedBridgePreset: "已应用 GPT-5.x 桥接预设",
        appliedBoundedPreset: "已应用有界故障转移预设",
        appliedInfinitePreset: "已应用无限恢复预设",
        unsavedChanges: "有未保存更改",
        noData: "暂无数据",
        noBridgeRules: "暂无桥接规则",
        noInterceptRules: "暂无拦截规则",
        noProviders: "暂无服务商配置",
        noHistoryVersions: "暂无历史版本",
        bridgeRule: "桥接规则 {index}",
        rule: "规则 {index}",
        version: "版本 {index}",
        remove: "移除",
        preview: "预览",
        collapse: "折叠",
        expand: "展开",
        probe: "探测",
        testing: "测试中",
        untested: "未测试",
        reachable: "可达",
        failed: "失败",
        target: "目标",
        status: "状态",
        healthy: "健康",
        degraded: "退化",
        quotaBlocked: "额度封禁",
        officialPriceUnavailable: "官方价格不可用",
        perMillion: "每百万",
        cached: "缓存",
        promptCache: "提示缓存",
        requestsShort: "请求",
        requestRows: "行",
        savedAt: "保存于",
        added: "新增",
        removed: "删除",
        changedBlocks: "变更块",
        diffPreview: "差异预览",
        class: "类别",
        models: "模型",
        count: "数量",
        weight: "权重",
        timeout: "超时",
        auth: "鉴权",
        providerName: "服务商名称",
        baseUrl: "基础 URL",
        apiKey: "API Key",
        providerClass: "服务商类别",
        providerClassHelp: "先选免费上游；命中拥塞后再回退到额度受限上游。",
        modelsLabel: "模型范围",
        headersLabel: "请求头",
        sameUpstreamRetries: "同上游重试",
        tokenSet: "已配置 token",
        noToken: "无 token",
        unscoped: "未限定",
        enabled: "启用",
        disabled: "停用",
        provider: "服务商 {index}"
      },
      en: {
        brandTitle: "AI MODEL GATEWAY",
        brandSubtitle: "Unified observability, routing, pricing, and runtime control.",
        overviewNavPerformance: "Performance",
        overviewNavEconomics: "Economics",
        overviewNavUpstreams: "Upstreams",
        overviewNavRequests: "Requests",
        overviewNavSettings: "Settings",
        settingsNavOverview: "Overview",
        performanceTitle: "Live Performance",
        performanceCaption: "Start with the real 1-minute and 5-minute RPM, TPM, and latency windows.",
        runtimePostureTitle: "Runtime Posture",
        runtimePostureCaption: "Current recovery mode, failure exit, and probe posture.",
        upstreamHealthTitle: "Upstream Health",
        upstreamHealthCaption: "See which upstream is slow, degraded, or cooling down before switching.",
        recentErrorsTitle: "Recent Errors",
        recentErrorsCaption: "Compressed view of ownership, status, and dominant error message.",
        recentRequestsTitle: "Recent Requests",
        recentRequestsCaption: "Latest request traces with status, attempts, latency, and estimated cost.",
        requestThroughputTitle: "Request Throughput",
        requestThroughputCaption: "RPM and TPM trends aggregated by time bucket.",
        latencyTrendTitle: "Latency Trend",
        latencyTrendCaption: "Average latency (ms) and success-rate trend.",
        successFailureTitle: "Success / Failure",
        successFailureCaption: "Successful and failed requests in each bucket.",
        tokenUsageTitle: "Token Usage by Upstream",
        tokenUsageCaption: "Token consumption grouped by upstream.",
        economicsTitle: "Model Economics",
        economicsCaption: "Model-level token usage, estimated USD cost, and pricing coverage.",
        costSnapshotTitle: "Cost Snapshot",
        costSnapshotCaption: "Estimated from known official model pricing.",
        upstreamUsageTitle: "Upstream Usage",
        upstreamUsageCaption: "Token usage by upstream, compared against health posture.",
        cacheRankingTitle: "Cache Hit Ranking",
        cacheRankingCaption: "Last-24h cache hit ranking by upstream.",
        runtimeConfigTitle: "Runtime Config",
        runtimeConfigCaption: "Manage probes, bridge, recovery, language, and providers in one place.",
        settingsNavTitle: "Sections",
        settingsHealth: "Health",
        settingsBridge: "Bridge",
        settingsRouter: "Router",
        settingsIntercepts: "Intercepts",
        settingsProviders: "Providers",
        settingsHistory: "History",
        settingsControlsTitle: "Controls",
        settingsHistoryTitle: "History",
        settingsSearchPlaceholder: "Search sections or providers...",
        settingsExpandAll: "Expand All",
        settingsCollapseAll: "Collapse All",
        saveConfig: "Save Config",
        reloadConfig: "Reload",
        exportConfig: "Export",
        rollbackConfig: "Rollback",
        allProviders: "All Providers",
        freeFirst: "Free First",
        quotaLimited: "Quota-Limited",
        languageLabel: "Interface Language",
        languageChinese: "Chinese",
        languageEnglish: "English",
        loading: "Loading",
        loaded: "Loaded",
        configSynced: "Config synced",
        loadConfigFailed: "Load config failed",
        loadingDiff: "Loading diff...",
        diffLoaded: "Diff loaded",
        loadDiffFailed: "Load diff failed",
        testingProvider: "Testing provider...",
        providerTestPassed: "Provider test passed",
        providerTestFailed: "Provider test failed",
        fixValidationErrors: "Fix validation errors before saving",
        saving: "Saving...",
        saved: "Saved",
        saveFailed: "Save failed",
        rollbackLatestConfirm: "Rollback to the latest saved config version?",
        rollbackSelectedConfirm: "Rollback to the selected config version?",
        rollingBack: "Rolling back...",
        rolledBack: "Rolled back",
        rollbackFailed: "Rollback failed",
        appliedBridgePreset: "Applied GPT-5.x bridge preset",
        appliedBoundedPreset: "Applied bounded failover preset",
        appliedInfinitePreset: "Applied infinite recovery preset",
        unsavedChanges: "Unsaved changes",
        noData: "No data yet",
        noBridgeRules: "No bridge rules yet",
        noInterceptRules: "No intercept rules yet",
        noProviders: "No provider configs yet",
        noHistoryVersions: "No config history yet",
        bridgeRule: "Bridge Rule {index}",
        rule: "Rule {index}",
        version: "Version {index}",
        remove: "Remove",
        preview: "Preview",
        collapse: "Collapse",
        expand: "Expand",
        probe: "Probe",
        testing: "Testing",
        untested: "Untested",
        reachable: "Reachable",
        failed: "Failed",
        target: "Target",
        status: "Status",
        healthy: "Healthy",
        degraded: "Degraded",
        quotaBlocked: "Quota Blocked",
        officialPriceUnavailable: "Official pricing unavailable",
        perMillion: "per 1M",
        cached: "cached",
        promptCache: "prompt cache",
        requestsShort: "reqs",
        requestRows: "rows",
        savedAt: "saved",
        added: "added",
        removed: "removed",
        changedBlocks: "changed blocks",
        diffPreview: "Diff Preview",
        class: "Class",
        models: "Models",
        count: "Count",
        weight: "Weight",
        timeout: "Timeout",
        auth: "Auth",
        providerName: "Provider Name",
        baseUrl: "Base URL",
        apiKey: "API Key",
        providerClass: "Provider Class",
        providerClassHelp: "Prefer free providers first; fall back to quota-limited only under pressure.",
        modelsLabel: "Models",
        headersLabel: "Headers",
        sameUpstreamRetries: "Same Upstream Retries",
        tokenSet: "token set",
        noToken: "no token",
        unscoped: "unscoped",
        enabled: "Enabled",
        disabled: "Disabled",
        provider: "Provider {index}"
      }
    };
    const t = (key, vars = {}) => {
      let text = (I18N[currentLocale] && I18N[currentLocale][key]) || I18N.zh[key] || key;
      for (const [name, value] of Object.entries(vars)) {
        text = text.replaceAll('{' + name + '}', String(value));
      }
      return text;
    };
    const localeText = (zh, en) => currentLocale === 'en' ? en : zh;
    const rebuildFormatters = () => {
      fmt = new Intl.NumberFormat(currentLocale === 'en' ? "en-US" : "zh-CN");
      fmtUsd = new Intl.NumberFormat("en-US", { style: "currency", currency: "USD", minimumFractionDigits: 4, maximumFractionDigits: 4 });
      rtf = new Intl.RelativeTimeFormat(currentLocale === 'en' ? 'en' : 'zh', { numeric: 'auto' });
    };
    let fmt = new Intl.NumberFormat(currentLocale === 'en' ? "en-US" : "zh-CN");
    let fmtUsd = new Intl.NumberFormat("en-US", { style: "currency", currency: "USD", minimumFractionDigits: 4, maximumFractionDigits: 4 });
    let rtf = new Intl.RelativeTimeFormat(currentLocale === 'en' ? 'en' : 'zh', { numeric: 'auto' });
    const relativeTime = (ts) => {
      if (!ts) return "-";
      const delta = Math.max(0, Date.now() - new Date(ts).getTime());
      if (delta < 60000) return rtf.format(-Math.round(delta / 1000), 'second');
      if (delta < 3600000) return rtf.format(-Math.round(delta / 60000), 'minute');
      return rtf.format(-Math.round(delta / 3600000), 'hour');
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
    const miniChip = (label, value, tone = '') => '<span class="mini-chip ' + tone + '"><strong>' + escapeHTML(label) + '</strong><span>' + escapeHTML(value) + '</span></span>';
    const surfaceCard = (label, value, meta = '', tone = '') => '<div class="surface-card ' + tone + '"><div class="surface-card-label">' + escapeHTML(label) + '</div><div class="surface-card-value mono">' + escapeHTML(value) + '</div><div class="surface-card-meta">' + escapeHTML(meta) + '</div></div>';
    const compactUsd = (value) => {
      const amount = Number(value || 0);
      if (amount >= 1000) return '$' + (amount / 1000).toFixed(1) + 'k';
      if (amount >= 100) return '$' + amount.toFixed(0);
      if (amount >= 1) return '$' + amount.toFixed(1);
      return fmtMoney(amount);
    };
    const routeModeLabel = (mode) => {
      const normalized = String(mode || 'direct').trim().toLowerCase();
      if (normalized === 'bridge') return localeText('桥接', 'bridge');
      return localeText('直连', 'direct');
    };
    const providerClassLabel = (providerClass) => String(providerClass || 'quota_limited').trim() === 'free'
      ? t('freeFirst')
      : t('quotaLimited');
    const statusChip = (statusCode) => {
      const code = Number(statusCode || 0);
      let tone = '';
      if (code >= 200 && code < 300) tone = 'ok';
      else if (code >= 400 && code < 500) tone = 'warn';
      else if (code >= 500) tone = 'danger';
      return '<span class="status-chip ' + tone + '">' + escapeHTML(code || '-') + '</span>';
    };
    const statusPill = (healthy) => healthy ? '<span class="status">healthy</span>' : '<span class="status bad">degraded</span>';
    const escapeHTML = (value) => String(value ?? '-')
      .replaceAll('&', '&amp;')
      .replaceAll('<', '&lt;')
      .replaceAll('>', '&gt;')
      .replaceAll('"', '&quot;')
      .replaceAll("'", '&#39;');
    const priceLine = (pricing) => {
      if (!pricing) return '<span class="small">' + escapeHTML(t('officialPriceUnavailable')) + '</span>';
      return '<span class="small mono">$' + pricing.input_per_1m_usd + ' / $' + pricing.output_per_1m_usd + ' ' + escapeHTML(t('perMillion')) + '</span>';
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
    const aggregateBy = (items, keyFn) => {
      const counts = new Map();
      (items || []).forEach((item) => {
        const key = String(keyFn(item) || '').trim();
        if (!key) return;
        counts.set(key, (counts.get(key) || 0) + 1);
      });
      return Array.from(counts.entries()).sort((a, b) => b[1] - a[1]);
    };
    const stackCell = (main, sub = '', extra = '') => '<div class="cell-stack"><div class="cell-main">' + main + '</div>' + (sub ? '<div class="cell-sub">' + sub + '</div>' : '') + extra + '</div>';
    const promptUsageCell = (usage) => {
      const promptTokens = fmt.format((usage && usage.prompt_tokens) || 0);
      const cachedTokens = (usage && usage.cached_prompt_tokens) || 0;
      if (!cachedTokens) return promptTokens;
      return promptTokens + '<br><span class="small">' + escapeHTML(t('cached')) + ' ' + fmt.format(cachedTokens) + '</span>';
    };
    const totalUsageCell = (usage) => {
      const totalTokens = fmt.format((usage && usage.total_tokens) || 0);
      const cachedTokens = (usage && usage.cached_prompt_tokens) || 0;
      if (!cachedTokens) return totalTokens;
      return totalTokens + '<br><span class="small">' + escapeHTML(t('promptCache')) + ' ' + fmt.format(cachedTokens) + '</span>';
    };
    const cacheHitRate = (usage) => {
      const promptTokens = Math.max(0, (usage && usage.prompt_tokens) || 0);
      const cachedTokens = Math.max(0, Math.min((usage && usage.cached_prompt_tokens) || 0, promptTokens));
      if (!promptTokens) return null;
      return (cachedTokens / promptTokens) * 100;
    };
    const cacheRateCell = (usage) => {
      const rate = cacheHitRate(usage);
      if (rate === null) return '<span class="small">' + escapeHTML(localeText('无', 'n/a')) + '</span>';
      const cachedTokens = Math.max(0, (usage && usage.cached_prompt_tokens) || 0);
      return fmtPct(rate) + '<br><span class="small">' + fmt.format(cachedTokens) + ' ' + escapeHTML(t('cached')) + '</span>';
    };
    const cacheTrendDetail = (window) => {
      const cached = fmt.format((window && window.cached_prompt_tokens) || 0);
      const prompt = fmt.format((window && window.prompt_tokens) || 0);
      const reqs = fmt.format((window && window.requests) || 0);
      return cached + ' / ' + prompt + ' prompt · ' + reqs + ' ' + escapeHTML(t('requestsShort'));
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
      if (!rows.length) return '<div class="small">' + escapeHTML(t('noData')) + '</div>';
      return '<div class="table-shell ' + className + '"><table><thead><tr>' + headers.map(h => '<th>' + h + '</th>').join('') + '</tr></thead><tbody>' + rows.map(row => '<tr>' + row.map(cell => '<td>' + cell + '</td>').join('') + '</tr>').join('') + '</tbody></table></div>';
    };
    const upstreamStatePill = (status) => {
      if (status && status.quota_blocked) return '<span class="status bad">' + escapeHTML(t('quotaBlocked')) + '</span>';
      if (status && !status.healthy) return '<span class="status bad">' + escapeHTML(t('degraded')) + '</span>';
      return '<span class="status">' + escapeHTML(t('healthy')) + '</span>';
    };
    const formatCooldown = (status) => {
      if (!status) return '-';
      if (status.quota_blocked) return t('quotaBlocked');
      if (status.cooldown_until && status.cooldown_until !== '0001-01-01T00:00:00Z') return relativeTime(status.cooldown_until);
      return '-';
    };
    const renderUpstreamHealth = (entries) => {
      if (!entries.length) return '<div class="small">' + escapeHTML(t('noData')) + '</div>';
      return '<div class="upstream-grid">' + entries.map(([name, status]) => {
        const safeStatus = status || {};
        const lastError = safeStatus.last_error ? escapeHTML(safeStatus.last_error) : (safeStatus.healthy ? localeText('最近无错误', 'no recent error') : localeText('无详情', 'no detail'));
        const latency = safeStatus.last_latency ? fmtMs((safeStatus.last_latency || 0) / 1000000) : localeText('无', 'n/a');
        const retryableFails = fmt.format(safeStatus.consecutive_retryable_failures || 0);
        const cooldown = formatCooldown(safeStatus);
        let tone = '';
        if (safeStatus.quota_blocked || !safeStatus.healthy) tone = ' is-degraded';
        else if ((safeStatus.last_latency || 0) / 1000000 > 3000) tone = ' is-warn';
        return '<article class="upstream-tile' + tone + '">'
          + '<div class="upstream-head">'
          + '<div class="upstream-heading">'
          + '<div class="upstream-name">' + escapeHTML(name) + '</div>'
          + '<div class="upstream-note">' + lastError + '</div>'
          + '</div>'
          + upstreamStatePill(safeStatus)
          + '</div>'
          + '<div class="upstream-stats">'
          + '<div class="upstream-stat"><div class="upstream-stat-label">' + escapeHTML(localeText('延迟', 'Latency')) + '</div><div class="upstream-stat-value mono">' + escapeHTML(latency) + '</div></div>'
          + '<div class="upstream-stat"><div class="upstream-stat-label">' + escapeHTML(localeText('失败', 'Failures')) + '</div><div class="upstream-stat-value mono">' + retryableFails + '</div></div>'
          + '<div class="upstream-stat"><div class="upstream-stat-label">' + escapeHTML(localeText('冷却', 'Cooldown')) + '</div><div class="upstream-stat-value">' + escapeHTML(cooldown) + '</div></div>'
          + '</div>'
          + '</article>';
      }).join('') + '</div>';
    };
    const errorFeed = (items) => {
      if (!items.length) return '<div class="small">' + escapeHTML(t('noData')) + '</div>';
      return '<div class="error-feed">' + items.slice(0, 12).map(item => {
        const code = Number(item.status_code || 0);
        const upstream = escapeHTML(item.upstream || '-');
        const model = escapeHTML(item.model || '-');
        const attempt = escapeHTML(item.attempt || '-');
        const message = escapeHTML(item.message || '-');
        const badge = code >= 400 ? '<span class="status bad">' + escapeHTML(code || '-') + '</span>' : '<span class="status">' + escapeHTML(code || '-') + '</span>';
        return '<article class="error-item">'
          + '<div class="error-top">'
          + '<div class="error-frame">'
          + '<div class="error-heading"><div class="error-title mono">' + escapeHTML(relativeTime(item.timestamp)) + '</div><div class="small">' + escapeHTML(localeText('尝试', 'attempt')) + ' ' + attempt + '</div></div>'
          + '<div class="error-context">'
          + '<div class="error-message">' + message + '</div>'
          + '<div class="small">' + upstream + ' · ' + model + '</div>'
          + '</div>'
          + '</div>'
          + badge
          + '</div>'
          + '<div class="error-meta">'
          + '<span class="tag">' + upstream + '</span>'
          + '<span class="tag">' + model + '</span>'
          + '<span class="tag accent">' + escapeHTML(routeModeLabel(item.route_mode)) + '</span>'
          + '<span class="tag">' + escapeHTML(localeText('状态', 'status')) + ' ' + escapeHTML(code || '-') + '</span>'
          + '</div>'
          + '</article>';
      }).join('') + '</div>';
    };
    const byId = (id) => document.getElementById(id);
    const setText = (selector, key) => {
      const el = document.querySelector(selector);
      if (el) el.textContent = t(key);
    };
    const setTextValue = (selector, value) => {
      const el = document.querySelector(selector);
      if (el) el.textContent = value;
    };
    const setPlaceholder = (selector, key) => {
      const el = document.querySelector(selector);
      if (el) el.setAttribute('placeholder', t(key));
    };
    const setPlaceholderValue = (selector, value) => {
      const el = document.querySelector(selector);
      if (el) el.setAttribute('placeholder', value);
    };
    const setAttrValue = (selector, attr, value) => {
      const el = document.querySelector(selector);
      if (el) el.setAttribute(attr, value);
    };
    const setCheckboxLabelText = (id, key) => {
      const input = byId(id);
      const label = input?.parentElement;
      if (!input || !label) return;
      Array.from(label.childNodes).forEach((node) => {
        if (node !== input) {
          node.remove();
        }
      });
      label.appendChild(document.createTextNode(' ' + t(key)));
    };
    const setCheckboxLabelValue = (id, value) => {
      const input = byId(id);
      const label = input?.parentElement;
      if (!input || !label) return;
      Array.from(label.childNodes).forEach((node) => {
        if (node !== input) {
          node.remove();
        }
      });
      label.appendChild(document.createTextNode(' ' + value));
    };
    const setSectionTitleValue = (id, value) => {
      const el = byId(id);
      if (el) el.setAttribute('data-section-title', value);
    };
    const applyStaticLocale = () => {
      document.documentElement.lang = currentLocale === 'en' ? 'en' : 'zh-CN';
      document.title = document.body.classList.contains('page-settings')
        ? (currentLocale === 'en' ? 'AI Gateway Settings' : 'AI 模型网关设置')
        : (currentLocale === 'en' ? 'AI Gateway Admin' : 'AI 模型网关管理台');
      setText('.brand-title', 'brandTitle');
      setText('.brand-subtitle', 'brandSubtitle');
      setText('#performance .title', 'performanceTitle');
      setText('#performance .caption', 'performanceCaption');
      setText('#runtime-card .title', 'runtimePostureTitle');
      setText('#runtime-card .caption', 'runtimePostureCaption');
      setText('#upstreams-card .title', 'upstreamHealthTitle');
      setText('#upstreams-card .caption', 'upstreamHealthCaption');
      setText('#errors-card .title', 'recentErrorsTitle');
      setText('#errors-card .caption', 'recentErrorsCaption');
      setText('#requests-card .title', 'recentRequestsTitle');
      setText('#requests-card .caption', 'recentRequestsCaption');
      setText('#chartLayout .card:nth-of-type(1) .title', 'requestThroughputTitle');
      setText('#chartLayout .card:nth-of-type(1) .caption', 'requestThroughputCaption');
      setText('#chartLayout .card:nth-of-type(2) .title', 'latencyTrendTitle');
      setText('#chartLayout .card:nth-of-type(2) .caption', 'latencyTrendCaption');
      setText('#chartLayout .card:nth-of-type(3) .title', 'successFailureTitle');
      setText('#chartLayout .card:nth-of-type(3) .caption', 'successFailureCaption');
      setText('#chartLayout .card:nth-of-type(4) .title', 'tokenUsageTitle');
      setText('#chartLayout .card:nth-of-type(4) .caption', 'tokenUsageCaption');
      setText('#economics .title', 'economicsTitle');
      setText('#economics .caption', 'economicsCaption');
      setText('#cost-card .title', 'costSnapshotTitle');
      setText('#cost-card .caption', 'costSnapshotCaption');
      setText('#usage-card .title', 'upstreamUsageTitle');
      setText('#usage-card .caption', 'upstreamUsageCaption');
      setText('#cache-card .title', 'cacheRankingTitle');
      setText('#cache-card .caption', 'cacheRankingCaption');
      setText('#runtimeConfig .title', 'runtimeConfigTitle');
      setText('#runtimeConfig .caption', 'runtimeConfigCaption');
      document.querySelectorAll('.topnav a[data-topnav-target]').forEach((link) => {
        const target = link.getAttribute('data-topnav-target');
        if (target === 'performance') link.textContent = t('overviewNavPerformance');
        if (target === 'economics') link.textContent = t('overviewNavEconomics');
        if (target === 'upstreams-card') link.textContent = t('overviewNavUpstreams');
        if (target === 'requests-card') link.textContent = t('overviewNavRequests');
      });
      setTextValue('.topnav a[href="/admin/settings"]', t('overviewNavSettings'));
      setTextValue('.topnav a[href="/admin"]', t('settingsNavOverview'));
      setTextValue('.eyebrow', document.body.classList.contains('page-settings') ? localeText('配置中心', 'Configuration Center') : localeText('AI 模型网关管理台', 'AI Gateway Admin'));
      setTextValue('.hero-copy h1', document.body.classList.contains('page-settings') ? localeText('运行路由、探活、服务商。', 'Runtime Routing, Health, Providers.') : localeText('运维、成本、吞吐。', 'Ops, Cost, Throughput.'));
      setTextValue('.hero-copy .sub', document.body.classList.contains('page-settings')
        ? localeText('集中维护探活、桥接、恢复和服务商，不再在多个面板里来回切换。', 'Manage probes, bridge, recovery, and providers in one place instead of jumping across surfaces.')
        : localeText('先看吞吐、延迟、错误和上游健康，再往下追成本与缓存。', 'Check throughput, latency, errors, and upstream health first, then drill into cost and cache.'));
      setText('#settingsNav .settings-nav-title', 'settingsNavTitle');
      setText('#cfg-history .config-card-title', 'settingsHistoryTitle');
      setText('[data-nav-target="cfg-health"] strong', 'settingsHealth');
      setText('[data-nav-target="cfg-bridge"] strong', 'settingsBridge');
      setText('[data-nav-target="cfg-router"] strong', 'settingsRouter');
      setText('[data-nav-target="cfg-intercepts"] strong', 'settingsIntercepts');
      setText('[data-nav-target="cfg-upstreams"] strong', 'settingsProviders');
      setText('[data-nav-target="cfg-history"] strong', 'settingsHistory');
      setText('.command-panel .config-card-title', 'settingsControlsTitle');
      setPlaceholder('#configSearch', 'settingsSearchPlaceholder');
      setText('#expandSections', 'settingsExpandAll');
      setText('#collapseSections', 'settingsCollapseAll');
      setText('#saveConfig', 'saveConfig');
      setText('#reloadConfig', 'reloadConfig');
      setText('#exportConfig', 'exportConfig');
      setText('#rollbackConfig', 'rollbackConfig');
      setSectionTitleValue('cfg-health', localeText('探活', 'Health Check'));
      setSectionTitleValue('cfg-bridge', localeText('模型桥接', 'Model Bridge'));
      setSectionTitleValue('cfg-router', localeText('路由恢复', 'Router Retry'));
      setSectionTitleValue('cfg-intercepts', localeText('响应拦截', 'Response Intercepts'));
      setSectionTitleValue('cfg-upstreams', localeText('服务商', 'Service Providers'));
      setTextValue('#cfg-health .section-kicker span', localeText('运行守护', 'Runtime Guardrail'));
      setTextValue('#cfg-health .config-card-head .config-card-title', localeText('探活检查', 'Health Check'));
      setTextValue('#cfg-health .config-card-head .config-help', localeText('控制主动探活的开关、间隔、超时和路径。', 'Control health polling, interval, timeout, and path.'));
      setTextValue('#cfg-health .config-field:nth-of-type(1) > label', localeText('启用', 'Enabled'));
      setCheckboxLabelValue('cfgHealthEnabled', localeText('启用探活检查', 'Health checks enabled'));
      setTextValue('#cfg-health .config-field:nth-of-type(2) > label', localeText('路径', 'Path'));
      setTextValue('#cfg-health .config-field:nth-of-type(3) > label', localeText('间隔（秒）', 'Interval (sec)'));
      setTextValue('#cfg-health .config-field:nth-of-type(4) > label', localeText('超时（毫秒）', 'Timeout (ms)'));
      setTextValue('#cfg-bridge .section-kicker span', localeText('改写入口', 'Rewrite Surface'));
      setTextValue('#cfg-bridge .config-card-head .config-card-title', localeText('模型桥接', 'Model Bridge'));
      setTextValue('#cfg-bridge .config-card-head .config-help', localeText('维护模型别名映射和需跳过桥接的 User-Agent。', 'Maintain alias rewrites and excluded bridge User-Agents.'));
      setTextValue('#cfg-bridge .config-field:nth-of-type(1) > label', localeText('启用', 'Enabled'));
      setCheckboxLabelValue('cfgBridgeEnabled', localeText('在路由前改写请求模型', 'Rewrite requested model before routing'));
      setTextValue('#cfg-bridge .config-field:nth-of-type(2) > label', localeText('排除的 User-Agent', 'Exclude User-Agents'));
      setTextValue('#applyCodexBridgePreset', localeText('应用 GPT-5.x 桥接预设', 'Apply GPT-5.x Bridge Preset'));
      setTextValue('#addBridgeRule', localeText('新增桥接规则', 'Add Bridge Rule'));
      setTextValue('#cfg-router .section-kicker span', localeText('流量策略', 'Traffic Strategy'));
      setTextValue('#cfg-router .config-card-head .config-card-title', localeText('路由恢复', 'Router Retry'));
      setTextValue('#cfg-router .config-card-head .config-help', localeText('控制重试次数、退避窗口和失败出口。', 'Control retry rounds, backoff window, and failure exit.'));
      setTextValue('#cfg-router .config-field:nth-of-type(1) > label', localeText('路由策略', 'Router Strategy'));
      setTextValue('#cfg-router .config-field:nth-of-type(2) > label', localeText('最大重试次数', 'Max Retries'));
      setTextValue('#cfg-router .config-field:nth-of-type(3) > label', localeText('退避基线（毫秒）', 'Backoff Base (ms)'));
      setTextValue('#cfg-router .config-field:nth-of-type(4) > label', localeText('退避上限（毫秒）', 'Backoff Max (ms)'));
      setTextValue('#cfg-router .config-field:nth-of-type(5) > label', localeText('失败阈值', 'Failure Threshold'));
      setTextValue('#cfg-router .config-field:nth-of-type(6) > label', localeText('冷却时间（秒）', 'Cooldown (sec)'));
      setTextValue('#cfg-router .config-field:nth-of-type(7) > label', localeText('透传窗口（秒）', 'Passthrough After (sec)'));
      setTextValue('.retry-focus .section-kicker span', localeText('恢复策略', 'Recovery Policy'));
      setTextValue('.retry-focus .config-card-head .config-card-title', localeText('重试策略', 'Retry Policy'));
      setTextValue('.retry-focus .config-card-head .config-help', localeText('命中状态码或关键字后触发重试，也可以切到“任何错误都无限重试”的恢复模式。', 'Retry matched status codes or keywords, or switch to infinite recovery for any error.'));
      setTextValue('#retryModeCard .policy-card-label', localeText('恢复模式', 'Recovery Mode'));
      setTextValue('#retryBackoffCard .policy-card-label', localeText('退避窗口', 'Backoff Window'));
      setTextValue('#retryPassthroughCard .policy-card-label', localeText('失败出口', 'Failure Exit'));
      setTextValue('#retryModePresetBounded .policy-card-label', localeText('预设', 'Preset'));
      setTextValue('#retryModePresetBounded .policy-card-value', localeText('有界故障转移', 'Bounded Failover'));
      setTextValue('#retryModePresetBounded .policy-card-meta', localeText('遵守最大重试次数，并在失败窗口后允许透传上游错误。', 'Respect max retries and allow passthrough after the configured failure window.'));
      setTextValue('#retryModePresetInfinite .policy-card-label', localeText('预设', 'Preset'));
      setTextValue('#retryModePresetInfinite .policy-card-value', localeText('无限恢复', 'Infinite Recovery'));
      setTextValue('#retryModePresetInfinite .policy-card-meta', localeText('传输、状态码和拦截到的错误都会持续重试，直到调用方取消。', 'Retry transport, status, and intercepted body failures until the caller cancels.'));
      setTextValue('.retry-focus .config-field:nth-of-type(1) > label', localeText('恢复模式', 'Recovery Mode'));
      setCheckboxLabelValue('cfgRetryInfiniteOnError', localeText('任何错误都无限重试', 'Infinite retry on any error'));
      setTextValue('.retry-focus .config-field:nth-of-type(1) .config-help', localeText('传输错误、响应状态错误和拦截到的响应体错误会持续重试，直到客户端取消。', 'Transport, response status, and intercepted body errors keep retrying until the client cancels.'));
      setTextValue('.retry-focus .config-field:nth-of-type(2) > label', localeText('状态码', 'Status Codes'));
      setTextValue('.retry-focus .config-field:nth-of-type(3) > label', localeText('最小状态码', 'Status Code Min'));
      setTextValue('.retry-focus .config-field:nth-of-type(4) > label', localeText('消息关键字', 'Message Keywords'));
      setTextValue('#cfg-intercepts .section-kicker span', localeText('响应陷阱', 'Response Traps'));
      setTextValue('#cfg-intercepts .config-card-head .config-card-title', localeText('响应拦截', 'Response Intercepts'));
      setTextValue('#cfg-intercepts .config-card-head .config-help', localeText('按路径、状态码或错误关键字提前判定重试或失败。', 'Decide retry / fail early by path, status code, or message keywords.'));
      setTextValue('#addIntercept', localeText('新增拦截规则', 'Add Intercept'));
      setTextValue('#cfg-upstreams .section-kicker span', localeText('服务商矩阵', 'Provider Matrix'));
      setTextValue('#cfg-upstreams .config-card-head .config-card-title', localeText('服务商', 'Service Providers'));
      setTextValue('#cfg-upstreams .config-card-head .config-help', localeText('维护上游 URL、API key、模型范围和超时；每一项都可先测再保存。', 'Manage provider URLs, API keys, model scopes, and timeouts; test each one before saving.'));
      setTextValue('#addUpstream', localeText('新增服务商', 'Add Provider'));
      setTextValue('.command-panel .section-kicker span', localeText('控制台', 'Control Deck'));
      const providerClassFilter = byId('providerClassFilter');
      if (providerClassFilter?.options?.length >= 3) {
        providerClassFilter.options[0].textContent = t('allProviders');
        providerClassFilter.options[1].textContent = t('freeFirst');
        providerClassFilter.options[2].textContent = t('quotaLimited');
      }
      const languageFilter = byId('cfgAdminLanguage');
      if (languageFilter?.options?.length >= 2) {
        languageFilter.options[0].textContent = t('languageChinese');
        languageFilter.options[1].textContent = t('languageEnglish');
        languageFilter.title = t('languageLabel');
        languageFilter.setAttribute('aria-label', t('languageLabel'));
      }
      setPlaceholderValue('#cfgRetryKeywords', localeText('rate limit\nupstream request failed', 'rate limit\nupstream request failed'));
    };
    const setLocale = (language) => {
      currentLocale = language === 'en' ? 'en' : 'zh';
      rebuildFormatters();
      applyStaticLocale();
    };
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
    const commonBridgePresetRules = () => ([
      { from: 'gpt-5.2', to: 'gpt-5.4' },
      { from: 'gpt-5.2-codex', to: 'gpt-5.4' },
      { from: 'gpt-5.3*', to: 'gpt-5.4' }
    ]);
    const mergeBridgeRules = (current, extra) => {
      const merged = new Map();
      [...(current || []), ...(extra || [])].forEach((rule) => {
        const from = String(rule?.from || '').trim();
        const to = String(rule?.to || '').trim();
        if (!from || !to) return;
        merged.set(from.toLowerCase() + '->' + to.toLowerCase(), { from, to });
      });
      return Array.from(merged.values());
    };
    const apiText = async (url, options = {}) => {
      const res = await fetch(url, {
        credentials: 'same-origin',
        cache: 'no-store',
        ...options
      });
      const text = await res.text();
      if (!res.ok) {
        throw new Error(text || ('HTTP ' + res.status));
      }
      return text;
    };
    const apiJSON = async (url, options = {}) => {
      const text = await apiText(url, options);
      return text ? JSON.parse(text) : {};
    };
    let loadedConfigSnapshot = '';
    let configHistoryVersionCount = 0;
    const updateActiveTopnav = () => {
      const links = Array.from(document.querySelectorAll('[data-topnav-target]'));
      if (!links.length || document.body.classList.contains('page-settings')) return;
      const sections = links
        .map((link) => byId(link.getAttribute('data-topnav-target') || ''))
        .filter(Boolean);
      let activeID = sections[0]?.id || '';
      let bestDelta = Number.POSITIVE_INFINITY;
      sections.forEach((section) => {
        const rect = section.getBoundingClientRect();
        const delta = Math.abs(rect.top - 120);
        if (rect.bottom > 100 && delta < bestDelta) {
          bestDelta = delta;
          activeID = section.id;
        }
      });
      links.forEach((link) => {
        link.classList.toggle('active', link.getAttribute('data-topnav-target') === activeID);
      });
    };
    const buildConfigPayload = () => ({
      admin: {
        language: byId('cfgAdminLanguage')?.value || currentLocale
      },
      health: {
        enabled: byId('cfgHealthEnabled')?.checked ?? false,
        interval_sec: readNumber(byId('cfgHealthInterval')),
        timeout_ms: readNumber(byId('cfgHealthTimeout')),
        path: byId('cfgHealthPath')?.value || ''
      },
      bridge: {
        enabled: byId('cfgBridgeEnabled')?.checked ?? false,
        exclude_user_agents: parseList(byId('cfgBridgeExcludeUA')?.value),
        rules: collectBridgeRules()
      },
      router: {
        strategy: byId('cfgRouterStrategy')?.value || 'health_weighted_rr',
        max_retries: readNumber(byId('cfgMaxRetries')),
        retry_backoff_ms: readNumber(byId('cfgBackoff')),
        retry_backoff_max_ms: readNumber(byId('cfgBackoffMax')),
        failure_threshold: readNumber(byId('cfgFailureThreshold')),
        cooldown_sec: readNumber(byId('cfgCooldown')),
        failure_passthrough_after_sec: readNumber(byId('cfgPassthrough'))
      },
      proxy: {
        retry: {
          infinite_on_error: byId('cfgRetryInfiniteOnError')?.checked ?? false,
          status_codes: parseCodes(byId('cfgRetryCodes')?.value),
          status_code_min: readOptionalNumber(byId('cfgRetryMin')),
          message_keywords: parseList(byId('cfgRetryKeywords')?.value)
        },
        intercepts: collectIntercepts()
      },
      upstreams: collectUpstreams()
    });
    const setConfigHintState = (message, tone = '') => {
      const hint = byId('configHint');
      if (!hint) return;
      hint.textContent = message || '';
      hint.classList.remove('is-dirty', 'is-saved');
      if (tone === 'dirty') hint.classList.add('is-dirty');
      if (tone === 'saved') hint.classList.add('is-saved');
    };
    const setNavMeta = (id, text, tone = '') => {
      const el = byId(id);
      if (!el) return;
      el.textContent = text;
      el.classList.remove('meta-good', 'meta-warn', 'meta-danger');
      if (tone) el.classList.add('meta-' + tone);
    };
    const currentProviderClassFilter = () => String(byId('providerClassFilter')?.value || 'all').trim();
    const computeSettingsDiagnostics = () => {
      const providers = collectUpstreams();
      const enabledProviders = providers.filter((provider) => provider.enabled !== false).length;
      const disabledProviders = providers.length - enabledProviders;
      const freeProviders = providers.filter((provider) => String(provider.provider_class || 'quota_limited').trim() === 'free').length;
      const quotaLimitedProviders = providers.filter((provider) => String(provider.provider_class || 'quota_limited').trim() !== 'free').length;
      const emptyKeys = providers.filter((provider) => !String(provider.api_key || '').trim()).length;
      const unscopedProviders = providers.filter((provider) => !(provider.models || []).length).length;
      const bridgeRules = collectBridgeRules().filter((rule) => String(rule?.from || '').trim() && String(rule?.to || '').trim()).length;
      const interceptRules = collectIntercepts().filter((rule) => rule && rule.enabled !== false).length;
      const healthEnabled = byId('cfgHealthEnabled')?.checked ?? false;
      const healthPath = String(byId('cfgHealthPath')?.value || '').trim();
      const retryInfinite = byId('cfgRetryInfiniteOnError')?.checked ?? false;
      const retryKeywords = parseList(byId('cfgRetryKeywords')?.value).length;
      const issueCount =
        (enabledProviders === 0 ? 1 : 0) +
        (emptyKeys > 0 ? 1 : 0) +
        (unscopedProviders > 0 ? 1 : 0) +
        (healthEnabled && !healthPath ? 1 : 0);
      return {
        providers,
        enabledProviders,
        disabledProviders,
        freeProviders,
        quotaLimitedProviders,
        emptyKeys,
        unscopedProviders,
        bridgeRules,
        interceptRules,
        healthEnabled,
        healthPath,
        retryInfinite,
        retryKeywords,
        issueCount,
      };
    };
    const updateActiveSettingsNav = () => {
      if (!document.body.classList.contains('page-settings')) return;
      const sections = topLevelSections().filter((section) => !section.classList.contains('hidden-search'));
      const links = Array.from(document.querySelectorAll('[data-nav-target]'));
      if (!links.length) return;
      let activeID = sections[0]?.id || '';
      let bestDelta = Number.POSITIVE_INFINITY;
      sections.forEach((section) => {
        const rect = section.getBoundingClientRect();
        const delta = Math.abs(rect.top - 128);
        if (rect.bottom > 120 && delta < bestDelta) {
          bestDelta = delta;
          activeID = section.id;
        }
      });
      links.forEach((link) => {
        link.classList.toggle('active', link.getAttribute('data-nav-target') === activeID);
      });
    };
    const updateSettingsSummary = () => {
      const diagnostics = computeSettingsDiagnostics();
      const providerCount = diagnostics.providers.length;
      const enabledProviders = diagnostics.enabledProviders;
      const bridgeCount = diagnostics.bridgeRules;
      const interceptCount = diagnostics.interceptRules;
      const retryInfinite = diagnostics.retryInfinite;
      const recoveryLabel = retryInfinite ? localeText('无限恢复', 'always recover') : localeText('有界', 'bounded');
      const backoffBase = readNumber(byId('cfgBackoff'));
      const backoffMax = readNumber(byId('cfgBackoffMax'));
      const passthroughAfter = readNumber(byId('cfgPassthrough'));
      const retryKeywordCount = diagnostics.retryKeywords;
      const retryCodeCount = parseCodes(byId('cfgRetryCodes')?.value).length;
      const routerStrategy = String(byId('cfgRouterStrategy')?.value || 'health_weighted_rr').trim() || 'health_weighted_rr';
      const rawHealthPath = String(byId('cfgHealthPath')?.value || '').trim();
      const healthPath = rawHealthPath || (diagnostics.healthEnabled ? localeText('路径缺失', 'path missing') : t('disabled'));
      if (byId('cfgHealthMeta')) byId('cfgHealthMeta').innerHTML = [
        miniChip(localeText('探活', 'Probe'), diagnostics.healthEnabled ? t('enabled') : t('disabled'), diagnostics.healthEnabled ? 'accent' : ''),
        miniChip(localeText('路径', 'Path'), healthPath, diagnostics.healthEnabled && !diagnostics.healthPath ? 'warn' : ''),
      ].join('');
      if (byId('cfgBridgeMeta')) byId('cfgBridgeMeta').innerHTML = [
        miniChip(localeText('规则', 'Rules'), fmt.format(bridgeCount), bridgeCount ? 'accent' : ''),
        miniChip(localeText('草稿', 'Draft'), bridgeCount ? localeText('已映射', 'mapped') : localeText('为空', 'empty'), bridgeCount ? 'accent' : 'warn'),
      ].join('');
      if (byId('cfgRouterMeta')) byId('cfgRouterMeta').innerHTML = [
        miniChip(localeText('策略', 'Strategy'), routerStrategy, 'accent'),
        miniChip(localeText('模式', 'Mode'), recoveryLabel, retryInfinite ? 'accent' : ''),
        miniChip(localeText('重试', 'Retries'), fmt.format(readNumber(byId('cfgMaxRetries')))),
      ].join('');
      if (byId('cfgRetryMeta')) byId('cfgRetryMeta').innerHTML = [
        miniChip(localeText('模式', 'Mode'), recoveryLabel, retryInfinite ? 'accent' : ''),
        miniChip(localeText('状态码', 'Codes'), fmt.format(retryCodeCount)),
        miniChip(localeText('关键字', 'Keywords'), fmt.format(retryKeywordCount), retryKeywordCount ? 'accent' : ''),
      ].join('');
      if (byId('cfgInterceptMeta')) byId('cfgInterceptMeta').innerHTML = [
        miniChip(localeText('规则', 'Rules'), fmt.format(interceptCount), interceptCount ? 'accent' : ''),
        miniChip(localeText('模式', 'Mode'), interceptCount ? localeText('启用中', 'active') : localeText('空闲', 'idle'), interceptCount ? 'accent' : ''),
      ].join('');
      if (byId('cfgUpstreamsMeta')) byId('cfgUpstreamsMeta').innerHTML = [
        miniChip(localeText('启用中', 'Enabled'), fmt.format(enabledProviders), enabledProviders ? 'accent' : 'danger'),
        miniChip(localeText('免费', 'Free'), fmt.format(diagnostics.freeProviders), diagnostics.freeProviders ? 'accent' : ''),
        miniChip(localeText('额度', 'Quota'), fmt.format(diagnostics.quotaLimitedProviders), diagnostics.quotaLimitedProviders ? '' : 'warn'),
        miniChip(localeText('缺鉴权', 'Needs Auth'), fmt.format(diagnostics.emptyKeys), diagnostics.emptyKeys ? 'warn' : ''),
        miniChip(localeText('未限模', 'Unscoped'), fmt.format(diagnostics.unscopedProviders), diagnostics.unscopedProviders ? 'warn' : ''),
      ].join('');
      setNavMeta('navMetaHealth', healthPath, diagnostics.healthEnabled ? (diagnostics.healthPath ? 'good' : 'warn') : '');
      setNavMeta('navMetaBridge', fmt.format(bridgeCount) + ' ' + localeText('条规则', 'rules'), bridgeCount ? 'good' : '');
      setNavMeta('navMetaRouter', retryInfinite ? (routerStrategy + ' · ' + localeText('无限', 'infinite')) : routerStrategy, 'good');
      setNavMeta('navMetaIntercepts', fmt.format(interceptCount) + ' ' + localeText('条规则', 'rules'), interceptCount ? 'good' : '');
      setNavMeta('navMetaProviders', fmt.format(providerCount) + ' ' + localeText('个服务商', 'providers'), enabledProviders === 0 ? 'danger' : ((diagnostics.emptyKeys || diagnostics.unscopedProviders) ? 'warn' : 'good'));
      setNavMeta('navMetaHistory', fmt.format(configHistoryVersionCount) + ' ' + localeText('个版本', 'versions'), configHistoryVersionCount ? 'good' : '');
      if (byId('retryModeValue')) byId('retryModeValue').textContent = recoveryLabel;
      if (byId('retryModeMeta')) byId('retryModeMeta').textContent = retryInfinite
        ? localeText('传输、状态码或拦截到的响应体错误都会持续重试。', 'Any transport, status, or intercepted body error keeps retrying.')
        : localeText('只有命中可重试状态码或消息关键字才会继续下一次尝试。', 'Only retryable status/message matches continue to the next attempt.');
      if (byId('retryBackoffValue')) byId('retryBackoffValue').textContent = fmt.format(backoffBase) + ' -> ' + fmt.format(backoffMax) + ' ms';
      if (byId('retryBackoffMeta')) byId('retryBackoffMeta').textContent = retryInfinite
        ? localeText('无限恢复下也会继续遵守退避窗口。', 'Backoff stays in effect across unlimited recovery attempts.')
        : localeText('有界重试窗口内执行指数退避。', 'Exponential backoff inside the bounded retry window.');
      if (byId('retryPassthroughValue')) byId('retryPassthroughValue').textContent = retryInfinite
        ? localeText('已抑制', 'suppressed')
        : localeText('在 ', 'after ') + fmt.format(passthroughAfter) + localeText(' 秒后', ' s');
      if (byId('retryPassthroughMeta')) byId('retryPassthroughMeta').textContent = retryInfinite
        ? localeText('无限模式会持续重试，而不会进入上游错误透传窗口。', 'Infinite mode keeps retrying instead of surfacing the upstream error window.')
        : localeText('到达配置窗口后，可透传可重试失败的上游响应。', 'Retryable failures can be surfaced after the configured window.');
      if (byId('cfgMaxRetriesHint')) byId('cfgMaxRetriesHint').textContent = retryInfinite
        ? localeText('已保存，但在无限恢复开启时不会生效。', 'Stored but ignored while Infinite Recovery is active.')
        : localeText('达到此次数后，网关停止继续重试。', 'Maximum retry rounds before the gateway stops retrying.');
      if (byId('cfgPassthroughHint')) byId('cfgPassthroughHint').textContent = retryInfinite
        ? localeText('已保存，但无限恢复会一直重试直到取消，因此该窗口不生效。', 'Stored but inactive while Infinite Recovery keeps retrying until cancel.')
        : localeText('超过该窗口后，可重试失败可以透传上游响应。', 'After this window, retryable failures can surface the upstream response.');
      byId('cfgMaxRetries')?.closest('.config-field')?.classList.toggle('subdued', retryInfinite);
      byId('cfgPassthrough')?.closest('.config-field')?.classList.toggle('subdued', retryInfinite);
      byId('retryModeCard')?.classList.toggle('active', retryInfinite);
      byId('retryBackoffCard')?.classList.toggle('active', backoffBase > 0 || backoffMax > 0);
      byId('retryPassthroughCard')?.classList.toggle('warn', retryInfinite);
      byId('retryModePresetBounded')?.classList.toggle('active', !retryInfinite);
      byId('retryModePresetInfinite')?.classList.toggle('active', retryInfinite);
      applyProviderClassFilter();
      updateActiveSettingsNav();
    };
    const ensureSectionControls = () => {
      topLevelSections().forEach((section) => {
        const head = section.querySelector('.config-card-head');
        if (!head || head.querySelector('.section-toggle')) return;
        const actions = document.createElement('div');
        actions.className = 'config-actions';
        actions.innerHTML = '<button class="btn secondary section-toggle" type="button">' + escapeHTML(t('collapse')) + '</button>';
        head.appendChild(actions);
      });
    };
    const setSectionCollapsed = (section, collapsed) => {
      section.classList.toggle('collapsed', !!collapsed);
      const button = section.querySelector('.section-toggle');
      if (button) {
        button.textContent = collapsed ? t('expand') : t('collapse');
      }
    };
    const setUpstreamCollapsed = (card, collapsed) => {
      if (!card) return;
      card.classList.toggle('collapsed', !!collapsed);
      const button = card.querySelector('.upstream-toggle');
      if (button) {
        button.textContent = collapsed ? t('expand') : t('collapse');
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
      applyProviderClassFilter();
      updateSettingsSummary();
    };
    const applyProviderClassFilter = () => {
      const providerClassFilter = currentProviderClassFilter();
      const cards = Array.from(document.querySelectorAll('[data-upstream-config]'));
      cards.forEach((card) => {
        const cardClass = String(card.getAttribute('data-upstream-class') || 'quota_limited').trim();
        const matched = providerClassFilter === 'all' || cardClass === providerClassFilter;
        card.classList.toggle('hidden-provider', !matched);
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
        html += '<div><strong>' + escapeHTML(localeText('保存已阻止：', 'Save blocked:')) + '</strong><ul class="validation-list">' + errors.map((msg) => '<li>' + escapeHTML(msg) + '</li>').join('') + '</ul></div>';
      }
      if (warnings.length) {
        html += '<div><strong>' + escapeHTML(localeText('警告：', 'Warnings:')) + '</strong><ul class="validation-list">' + warnings.map((msg) => '<li>' + escapeHTML(msg) + '</li>').join('') + '</ul></div>';
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
      const collapsedCard = el.closest('.config-card.collapsed');
      if (collapsedCard) {
        collapsedCard.classList.remove('collapsed');
        const sectionButton = collapsedCard.querySelector('.section-toggle');
        if (sectionButton) sectionButton.textContent = t('collapse');
        const providerButton = collapsedCard.querySelector('.upstream-toggle');
        if (providerButton) providerButton.textContent = t('collapse');
      }
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
        errors.push(localeText('启用探活检查时必须填写路径。', 'Health Check path is required when health checks are enabled.'));
        markInvalid(healthPath, localeText('启用探活检查时必须填写路径。', 'Path is required while health checks are enabled.'));
      }

      const bridgeCards = Array.from(document.querySelectorAll('[data-bridge-rule]'));
      bridgeCards.forEach((card, idx) => {
        const from = card.querySelector('.bridge-from');
        const to = card.querySelector('.bridge-to');
        const fromValue = String(from?.value || '').trim();
        const toValue = String(to?.value || '').trim();
        if (!fromValue || !toValue) {
          errors.push(localeText('桥接规则 ', 'Bridge Rule ') + (idx + 1) + localeText(' 必须同时填写来源模型和目标模型。', ' requires both From and To.'));
          if (!fromValue) markInvalid(from, localeText('必须填写桥接来源模型。', 'Bridge source model is required.'));
          if (!toValue) markInvalid(to, localeText('必须填写桥接目标模型。', 'Bridge target model is required.'));
        }
      });

      const retryMin = byId('cfgRetryMin');
      const retryCodesInput = byId('cfgRetryCodes');
      const retryMinValue = readOptionalNumber(retryMin);
      if (retryMinValue !== null && retryMinValue < 100) {
        errors.push(localeText('重试策略的最小状态码必须大于等于 100。', 'Retry Policy status code min must be at least 100.'));
        markInvalid(retryMin, localeText('最小状态码必须在 100 到 599 之间。', 'Minimum retry status code must be between 100 and 599.'));
      }
      parseList(retryCodesInput?.value).forEach((raw) => {
        const value = Number.parseInt(raw, 10);
        if (!Number.isFinite(value) || value < 100 || value > 599) {
          errors.push(localeText('重试策略包含非法状态码：', 'Retry Policy contains an invalid status code: ') + raw + '.');
          markInvalid(retryCodesInput, localeText('重试状态码必须是 100 到 599 之间的整数。', 'Retry status codes must be integers between 100 and 599.'));
        }
      });

      Array.from(document.querySelectorAll('[data-intercept]')).forEach((card, idx) => {
        const codesInput = card.querySelector('.intercept-codes');
        const minInput = card.querySelector('.intercept-min');
        parseList(codesInput?.value).forEach((raw) => {
          const value = Number.parseInt(raw, 10);
          if (!Number.isFinite(value) || value < 100 || value > 599) {
            errors.push(localeText('响应拦截规则 ', 'Response Intercept ') + (idx + 1) + localeText(' 包含非法状态码：', ' contains an invalid status code: ') + raw + '.');
            markInvalid(codesInput, localeText('拦截状态码必须是 100 到 599 之间的整数。', 'Intercept status codes must be integers between 100 and 599.'));
          }
        });
        const minValue = readOptionalNumber(minInput);
        if (minValue !== null && minValue < 100) {
          errors.push(localeText('响应拦截规则 ', 'Response Intercept ') + (idx + 1) + localeText(' 的最小状态码必须大于等于 100。', ' status code min must be at least 100.'));
          markInvalid(minInput, localeText('拦截最小状态码必须在 100 到 599 之间。', 'Intercept minimum status code must be between 100 and 599.'));
        }
      });

      const upstreamCards = Array.from(document.querySelectorAll('[data-upstream-config]'));
      const seenNames = new Map();
      let enabledCount = 0;
      upstreamCards.forEach((card, idx) => {
        const nameInput = card.querySelector('.upstream-name');
        const baseURLInput = card.querySelector('.upstream-base-url');
        const keyInput = card.querySelector('.upstream-api-key');
        const classInput = card.querySelector('.upstream-provider-class');
        const modelsInput = card.querySelector('.upstream-models');
        const enabled = card.querySelector('.upstream-enabled')?.checked ?? true;
        const name = String(nameInput?.value || '').trim();
        const baseURL = String(baseURLInput?.value || '').trim();
        const providerClass = String(classInput?.value || 'quota_limited').trim();
        const models = parseList(modelsInput?.value);

        if (enabled) enabledCount++;
        if (!name) {
          errors.push(localeText('服务商 ', 'Service Provider ') + (idx + 1) + localeText(' 必须填写名称。', ' requires a provider name.'));
          markInvalid(nameInput, localeText('必须填写服务商名称。', 'Provider name is required.'));
        } else {
          const key = name.toLowerCase();
          if (seenNames.has(key)) {
            errors.push(localeText('服务商名称重复：', 'Duplicate provider name: ') + name + '.');
            markInvalid(nameInput, localeText('服务商名称必须唯一。', 'Provider name must be unique.'));
            markInvalid(seenNames.get(key), localeText('服务商名称必须唯一。', 'Provider name must be unique.'));
          } else {
            seenNames.set(key, nameInput);
          }
        }
        if (!baseURL || !isValidHTTPURL(baseURL)) {
          errors.push(localeText('服务商 ', 'Service Provider ') + (idx + 1) + localeText(' 必须填写有效的 http/https Base URL。', ' requires a valid http/https Base URL.'));
          markInvalid(baseURLInput, localeText('Base URL 必须以 http:// 或 https:// 开头。', 'Base URL must start with http:// or https://.'));
        }
        if (providerClass !== 'free' && providerClass !== 'quota_limited') {
          errors.push(localeText('服务商 ', 'Service Provider ') + (idx + 1) + localeText(' 的类别只能是 free 或 quota_limited。', ' provider class must be free or quota_limited.'));
          markInvalid(classInput, localeText('服务商类别只能是 free 或 quota_limited。', 'Provider class must be free or quota_limited.'));
        }
        if (!String(keyInput?.value || '').trim()) {
          warnings.push(localeText('服务商 ', 'Service Provider ') + (idx + 1) + localeText(' 的 API key 为空。', ' has an empty API key.'));
          markInvalid(keyInput, localeText('API key 为空。如果该上游需要鉴权，请求可能失败。', 'API key is empty. Requests may fail if this upstream requires auth.'), true);
        }
        if (!models.length) {
          warnings.push(localeText('服务商 ', 'Service Provider ') + (idx + 1) + localeText(' 未配置模型范围。', ' has no model scope configured.'));
          markInvalid(modelsInput, localeText('未配置模型范围，该服务商不会命中按模型路由。', 'No models configured. This provider will not match model-based routing.'), true);
        }
      });

      if (!upstreamCards.length) {
        errors.push(localeText('至少需要一个服务商配置。', 'At least one service provider is required.'));
      } else if (enabledCount === 0) {
        errors.push(localeText('至少需要启用一个服务商。', 'At least one service provider must be enabled.'));
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
        list.innerHTML = '<div class="small">' + escapeHTML(t('noBridgeRules')) + '</div>';
        return;
      }
      list.innerHTML = items.map((rule, idx) => {
        return '' +
          '<div class="config-card" data-bridge-rule data-bridge-index="' + escapeHTML(idx) + '">' +
            '<div class="config-card-head">' +
              '<div class="config-card-title">' + escapeHTML(t('bridgeRule', { index: idx + 1 })) + '</div>' +
              '<div class="config-actions">' +
                '<button class="btn danger bridge-rule-remove" type="button">' + escapeHTML(t('remove')) + '</button>' +
              '</div>' +
            '</div>' +
            '<div class="config-grid">' +
              '<div class="config-field">' +
                '<label>' + escapeHTML(localeText('来源模型', 'From')) + '</label>' +
                '<input type="text" class="bridge-from" placeholder="gpt-5.2" value="' + escapeHTML(rule.from || '') + '">' +
              '</div>' +
              '<div class="config-field">' +
                '<label>' + escapeHTML(localeText('目标模型', 'To')) + '</label>' +
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
        list.innerHTML = '<div class="small">' + escapeHTML(t('noInterceptRules')) + '</div>';
        return;
      }
      list.innerHTML = items.map((rule, idx) => {
        const enabled = rule.enabled === false ? '' : 'checked';
        const action = String(rule.action).toLowerCase();
        return '' +
          '<div class="config-card" data-intercept>' +
            '<div class="config-card-head">' +
              '<div class="config-card-title">' + escapeHTML(t('rule', { index: idx + 1 })) + '</div>' +
              '<div class="config-actions">' +
                '<label class="small"><input type="checkbox" class="intercept-enabled" ' + enabled + '> ' + escapeHTML(t('enabled')) + '</label>' +
                '<button class="btn danger intercept-remove" type="button">' + escapeHTML(t('remove')) + '</button>' +
              '</div>' +
            '</div>' +
            '<div class="config-grid">' +
              '<div class="config-field">' +
                '<label>' + escapeHTML(localeText('名称', 'Name')) + '</label>' +
                '<input type="text" class="intercept-name" value="' + escapeHTML(rule.name || '') + '">' +
              '</div>' +
              '<div class="config-field">' +
                '<label>' + escapeHTML(localeText('动作', 'Action')) + '</label>' +
                '<select class="intercept-action">' +
                  '<option value="retry" ' + (action === 'retry' ? 'selected' : '') + '>' + escapeHTML(localeText('重试', 'retry')) + '</option>' +
                  '<option value="fail" ' + (action === 'fail' ? 'selected' : '') + '>' + escapeHTML(localeText('失败', 'fail')) + '</option>' +
                '</select>' +
              '</div>' +
              '<div class="config-field">' +
                '<label>' + escapeHTML(localeText('路径', 'Paths')) + '</label>' +
                '<input type="text" class="intercept-paths" placeholder="/v1/responses, /v1/chat/*" value="' + escapeHTML(listToString(rule.paths || [])) + '">' +
              '</div>' +
              '<div class="config-field">' +
                '<label>' + escapeHTML(localeText('状态码', 'Status Codes')) + '</label>' +
                '<input type="text" class="intercept-codes" placeholder="429,502" value="' + escapeHTML(listToString(rule.status_codes || [])) + '">' +
              '</div>' +
              '<div class="config-field">' +
                '<label>' + escapeHTML(localeText('最小状态码', 'Status Code Min')) + '</label>' +
                '<input type="number" min="0" class="intercept-min" value="' + (rule.status_code_min ?? '') + '">' +
              '</div>' +
              '<div class="config-field" style="grid-column: span 2;">' +
                '<label>' + escapeHTML(localeText('消息关键字', 'Message Keywords')) + '</label>' +
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
    const collectUpstreamCard = (card) => ({
      name: card.querySelector('.upstream-name')?.value || '',
      base_url: card.querySelector('.upstream-base-url')?.value || '',
      api_key: card.querySelector('.upstream-api-key')?.value || '',
      provider_class: card.querySelector('.upstream-provider-class')?.value || 'quota_limited',
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
      const probeValue = card.querySelector('.provider-summary-probe');
      if (pending) {
        if (probeValue) {
          probeValue.textContent = t('testing');
          probeValue.className = 'provider-summary-value provider-summary-probe probe-idle';
        }
        host.innerHTML = '<div class="probe-status"><div class="probe-summary"><span class="status">' + escapeHTML(t('testing')) + '</span><span class="small">' + escapeHTML(t('testingProvider')) + '</span></div></div>';
        return;
      }
      if (!result) {
        if (probeValue) {
          probeValue.textContent = t('untested');
          probeValue.className = 'provider-summary-value provider-summary-probe probe-idle';
        }
        host.innerHTML = '';
        return;
      }
      const kind = result.ok ? 'ok' : 'fail';
      const state = result.ok ? '<span class="status">' + escapeHTML(t('reachable')) + '</span>' : '<span class="status bad">' + escapeHTML(t('failed')) + '</span>';
      const target = result.target_url ? '<div class="small">' + escapeHTML(t('target')) + ' · ' + escapeHTML(result.target_url) + '</div>' : '';
      const preview = result.body_preview ? '<div class="probe-preview">' + escapeHTML(result.body_preview) + '</div>' : '';
      const detailBits = [
        result.status_code ? '<span class="tag">' + escapeHTML(localeText('状态', 'status')) + ' ' + escapeHTML(result.status_code) + '</span>' : '',
        result.latency_ms ? '<span class="tag">' + escapeHTML(result.latency_ms) + ' ms</span>' : '',
        result.checked_at ? '<span class="tag">' + escapeHTML(relativeTime(result.checked_at)) + '</span>' : ''
      ].filter(Boolean).join('');
      if (probeValue) {
        const probeText = result.ok
          ? (localeText('可达', 'ok') + (result.latency_ms ? ' · ' + result.latency_ms + ' ms' : ''))
          : (result.status_code ? (t('failed') + ' · ' + result.status_code) : t('failed'));
        probeValue.textContent = probeText;
        probeValue.className = 'provider-summary-value provider-summary-probe ' + (result.ok ? 'probe-ok' : 'probe-fail');
      }
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
        list.innerHTML = '<div class="small">' + escapeHTML(t('noProviders')) + '</div>';
        updateSettingsSummary();
        return;
      }
      list.innerHTML = items.map((upstream, idx) => {
        const enabled = upstream.enabled === false ? '' : 'checked';
        const title = escapeHTML(upstream.name || t('provider', { index: idx + 1 }));
        const base = escapeHTML(upstream.base_url || (localeText('基础 URL 未设置', 'Base URL not set')));
        const providerClass = String(upstream.provider_class || 'quota_limited').trim() === 'free' ? 'free' : 'quota_limited';
        const models = upstream.models || [];
        const modelCount = models.length;
        const modelPreview = escapeHTML(modelCount ? models.slice(0, 2).join(', ') + (modelCount > 2 ? ' +' + (modelCount - 2) : '') : t('unscoped'));
        const authMode = String(upstream.api_key || '').trim() ? t('tokenSet') : t('noToken');
        const collapsed = idx === 0 ? '' : ' collapsed';
        const enabledChip = upstream.enabled === false
          ? '<span class="provider-chip warn">' + escapeHTML(t('disabled')) + '</span>'
          : '<span class="provider-chip accent">' + escapeHTML(t('enabled')) + '</span>';
        return '' +
          '<div class="config-card provider-card' + collapsed + '" data-upstream-config data-upstream-name="' + escapeHTML(String(upstream.name || '').toLowerCase()) + '" data-upstream-class="' + escapeHTML(providerClass) + '">' +
            '<div class="config-card-head">' +
              '<div class="config-card-head-main">' +
                '<div class="config-card-title">' + title + '</div>' +
                '<div class="config-help">' + base + '</div>' +
              '</div>' +
              '<div class="config-actions">' +
                '<label class="small"><input type="checkbox" class="upstream-enabled" ' + enabled + '> ' + escapeHTML(t('enabled')) + '</label>' +
                '<button class="btn secondary upstream-toggle" type="button">' + escapeHTML(idx === 0 ? t('collapse') : t('expand')) + '</button>' +
                '<button class="btn secondary upstream-test" type="button">' + escapeHTML(t('probe')) + '</button>' +
                '<button class="btn danger upstream-remove" type="button">' + escapeHTML(t('remove')) + '</button>' +
              '</div>' +
            '</div>' +
            '<div class="provider-summary-strip">' +
              '<div class="provider-summary-item"><div class="provider-summary-label">' + escapeHTML(t('status')) + '</div><div class="provider-summary-value">' + enabledChip + '</div></div>' +
              '<div class="provider-summary-item"><div class="provider-summary-label">' + escapeHTML(t('class')) + '</div><div class="provider-summary-value">' + escapeHTML(providerClassLabel(providerClass)) + '</div></div>' +
              '<div class="provider-summary-item"><div class="provider-summary-label">' + escapeHTML(t('models')) + '</div><div class="provider-summary-value">' + modelPreview + '</div></div>' +
              '<div class="provider-summary-item"><div class="provider-summary-label">' + escapeHTML(t('count')) + '</div><div class="provider-summary-value">' + escapeHTML(modelCount) + '</div></div>' +
              '<div class="provider-summary-item"><div class="provider-summary-label">' + escapeHTML(t('weight')) + '</div><div class="provider-summary-value">' + escapeHTML(upstream.weight ?? 0) + '</div></div>' +
              '<div class="provider-summary-item"><div class="provider-summary-label">' + escapeHTML(t('timeout')) + '</div><div class="provider-summary-value">' + escapeHTML(upstream.timeout_ms ?? 0) + ' ms</div></div>' +
              '<div class="provider-summary-item"><div class="provider-summary-label">' + escapeHTML(t('auth')) + '</div><div class="provider-summary-value">' + escapeHTML(authMode) + '</div></div>' +
              '<div class="provider-summary-item"><div class="provider-summary-label">' + escapeHTML(t('probe')) + '</div><div class="provider-summary-value provider-summary-probe probe-idle">' + escapeHTML(t('untested')) + '</div></div>' +
            '</div>' +
            '<div class="config-grid">' +
              '<div class="config-field">' +
                '<label>' + escapeHTML(t('providerName')) + '</label>' +
                '<input type="text" class="upstream-name" value="' + escapeHTML(upstream.name || '') + '">' +
              '</div>' +
              '<div class="config-field">' +
                '<label>' + escapeHTML(t('baseUrl')) + '</label>' +
                '<input type="text" class="upstream-base-url" placeholder="https://api.example.com" value="' + escapeHTML(upstream.base_url || '') + '">' +
              '</div>' +
              '<div class="config-field" style="grid-column: span 2;">' +
                '<label>' + escapeHTML(t('apiKey')) + '</label>' +
                '<input type="text" class="upstream-api-key" placeholder="sk-..." value="' + escapeHTML(upstream.api_key || '') + '">' +
              '</div>' +
              '<div class="config-field">' +
                '<label>' + escapeHTML(t('providerClass')) + '</label>' +
                '<select class="upstream-provider-class">' +
                  '<option value="free" ' + (providerClass === 'free' ? 'selected' : '') + '>' + escapeHTML(t('freeFirst')) + '</option>' +
                  '<option value="quota_limited" ' + (providerClass === 'quota_limited' ? 'selected' : '') + '>' + escapeHTML(t('quotaLimited')) + '</option>' +
                '</select>' +
                '<div class="config-help">' + escapeHTML(t('providerClassHelp')) + '</div>' +
              '</div>' +
              '<div class="config-field">' +
                '<label>' + escapeHTML(t('modelsLabel')) + '</label>' +
                '<textarea class="upstream-models" placeholder="gpt-5.2&#10;gpt-5.2-codex">' + escapeHTML((upstream.models || []).join("\n")) + '</textarea>' +
              '</div>' +
              '<div class="config-field">' +
                '<label>' + escapeHTML(t('headersLabel')) + '</label>' +
                '<textarea class="upstream-headers" placeholder="X-Org: demo&#10;X-Region: cn">' + escapeHTML(headersToString(upstream.headers || {})) + '</textarea>' +
              '</div>' +
              '<div class="config-field">' +
                '<label>' + escapeHTML(t('weight')) + '</label>' +
                '<input type="number" min="0" class="upstream-weight" value="' + (upstream.weight ?? 0) + '">' +
              '</div>' +
              '<div class="config-field">' +
                '<label>' + escapeHTML(t('timeout')) + ' (ms)</label>' +
                '<input type="number" min="0" class="upstream-timeout" value="' + (upstream.timeout_ms ?? 0) + '">' +
              '</div>' +
              '<div class="config-field">' +
                '<label>' + escapeHTML(t('sameUpstreamRetries')) + '</label>' +
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
      configHistoryVersionCount = items.length;
      if (!items.length) {
        list.innerHTML = '<div class="small">' + escapeHTML(t('noHistoryVersions')) + '</div>';
        byId('configDiffPreview').innerHTML = '';
        updateSettingsSummary();
        return;
      }
      list.innerHTML = '<div class="history-list">' + items.map((item, idx) => {
        return '' +
          '<div class="history-item">' +
            '<div class="history-meta">' +
              '<div class="history-name">' + escapeHTML(item.filename || item.id || t('version', { index: idx + 1 })) + '</div>' +
              '<div class="small">' + escapeHTML(t('savedAt')) + ' ' + escapeHTML(relativeTime(item.created_at)) + ' · ' + escapeHTML(fmtBytes(item.size)) + '</div>' +
            '</div>' +
            '<div class="config-actions">' +
              '<button class="btn secondary history-preview" type="button" data-version-id="' + escapeHTML(item.id || '') + '">' + escapeHTML(t('preview')) + '</button>' +
              '<button class="btn danger history-rollback" type="button" data-version-id="' + escapeHTML(item.id || '') + '">' + escapeHTML(t('rollbackConfig')) + '</button>' +
            '</div>' +
          '</div>';
      }).join('') + '</div>';
      updateSettingsSummary();
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
            '<div class="config-card-title">' + escapeHTML(localeText('差异预览', 'Diff Preview')) + '</div>' +
            '<div class="config-help">' + escapeHTML(payload.version?.filename || '') + '</div>' +
          '</div>' +
          '<div class="diff-summary">' +
            '<span class="tag accent">+' + escapeHTML(summary.added_lines || 0) + ' ' + escapeHTML(t('added')) + '</span>' +
            '<span class="tag">' + escapeHTML(summary.removed_lines || 0) + ' ' + escapeHTML(t('removed')) + '</span>' +
            '<span class="tag">' + escapeHTML(summary.changed_blocks || 0) + ' ' + escapeHTML(t('changedBlocks')) + '</span>' +
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
        const payload = await apiJSON('/-/admin/config/history');
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
      byId('configHint').textContent = t('loadingDiff');
      try {
        const payload = await apiJSON('/-/admin/config/history/' + encodeURIComponent(versionId) + '/diff');
        renderConfigDiffPreview(payload);
        byId('configHint').textContent = t('diffLoaded');
      } catch (err) {
        byId('configHint').textContent = String(err?.message || err || t('loadDiffFailed'));
      }
    };
    const loadConfig = async () => {
      byId('configStatus').textContent = t('loading');
      try {
        const cfg = await apiJSON('/-/admin/config');
        setLocale(cfg.admin?.language || currentLocale);
        if (byId('cfgAdminLanguage')) byId('cfgAdminLanguage').value = cfg.admin?.language || currentLocale;
        byId('cfgHealthEnabled').checked = !!cfg.health?.enabled;
        byId('cfgHealthInterval').value = cfg.health?.interval_sec ?? 0;
        byId('cfgHealthTimeout').value = cfg.health?.timeout_ms ?? 0;
        byId('cfgHealthPath').value = cfg.health?.path || '';

        byId('cfgBridgeEnabled').checked = !!cfg.bridge?.enabled;
        byId('cfgBridgeExcludeUA').value = keywordsToString(cfg.bridge?.exclude_user_agents || []);
        renderBridgeRules(cfg.bridge?.rules || []);

        byId('cfgRouterStrategy').value = cfg.router?.strategy || 'health_weighted_rr';
        byId('cfgMaxRetries').value = cfg.router?.max_retries ?? 0;
        byId('cfgBackoff').value = cfg.router?.retry_backoff_ms ?? 0;
        byId('cfgBackoffMax').value = cfg.router?.retry_backoff_max_ms ?? 0;
        byId('cfgFailureThreshold').value = cfg.router?.failure_threshold ?? 0;
        byId('cfgCooldown').value = cfg.router?.cooldown_sec ?? 0;
        byId('cfgPassthrough').value = cfg.router?.failure_passthrough_after_sec ?? 0;

        const retry = cfg.proxy?.retry || {};
        byId('cfgRetryInfiniteOnError').checked = !!retry.infinite_on_error;
        byId('cfgRetryCodes').value = listToString(retry.status_codes || []);
        byId('cfgRetryMin').value = retry.status_code_min ?? '';
        byId('cfgRetryKeywords').value = keywordsToString(retry.message_keywords || []);

        renderIntercepts(cfg.proxy?.intercepts || []);
        renderUpstreamsConfig(cfg.upstreams || []);
        await loadConfigHistory();
        ensureSectionControls();
        applyConfigSearch();
        clearValidationState();
        loadedConfigSnapshot = JSON.stringify(buildConfigPayload());
        updateSettingsSummary();
        byId('configStatus').textContent = t('loaded');
        setConfigHintState(t('configSynced'), 'saved');
      } catch (err) {
        byId('configStatus').textContent = t('loadConfigFailed');
        setConfigHintState(String(err?.message || err || t('loadConfigFailed')));
      }
    };
    const testUpstreamCard = async (card) => {
      if (!card) return;
      renderUpstreamProbe(card, null, true);
      setConfigHintState(t('testingProvider'));
      try {
        const payload = await apiJSON('/-/admin/upstreams/test', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ upstream: collectUpstreamCard(card) })
        });
        renderUpstreamProbe(card, payload, false);
        setConfigHintState(payload.ok ? t('providerTestPassed') : t('providerTestFailed'), payload.ok ? 'saved' : '');
      } catch (err) {
        renderUpstreamProbe(card, { ok: false, error: String(err?.message || err || localeText('探测失败', 'probe failed')) }, false);
        setConfigHintState(String(err?.message || err || t('providerTestFailed')));
      }
    };
    const saveConfig = async () => {
      const validation = validateConfigForm();
      if (!validation.valid) {
        setConfigHintState(t('fixValidationErrors'));
        return;
      }
      const payload = buildConfigPayload();

      setConfigHintState(t('saving'));
      try {
        await apiJSON('/-/admin/config', {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(payload)
        });
        await loadConfig();
        setConfigHintState(t('saved'), 'saved');
      } catch (err) {
        setConfigHintState(String(err?.message || err || t('saveFailed')));
      }
    };
    const exportConfig = () => {
      window.open('/-/admin/config/export', '_blank', 'noopener,noreferrer');
    };
    const rollbackConfig = async (versionId = '') => {
      const confirmText = versionId
        ? t('rollbackSelectedConfirm')
        : t('rollbackLatestConfirm');
      if (!window.confirm(confirmText)) {
        return;
      }
      setConfigHintState(t('rollingBack'));
      try {
        await apiJSON('/-/admin/config/rollback', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ version_id: versionId })
        });
        await loadConfig();
        setConfigHintState(t('rolledBack'), 'saved');
      } catch (err) {
        setConfigHintState(String(err?.message || err || t('rollbackFailed')));
      }
    };
    document.addEventListener('click', (event) => {
      const button = event.target instanceof Element ? event.target.closest('button') : null;
      if (button && button.id === 'applyCodexBridgePreset') {
        event.preventDefault();
        renderBridgeRules(mergeBridgeRules(collectBridgeRules(), commonBridgePresetRules()));
        byId('cfgBridgeEnabled').checked = true;
        clearValidationState();
        updateSettingsSummary();
        setConfigHintState(t('appliedBridgePreset'), 'dirty');
      }
      if (button && button.id === 'retryModePresetBounded') {
        event.preventDefault();
        byId('cfgRetryInfiniteOnError').checked = false;
        clearValidationState();
        updateSettingsSummary();
        setConfigHintState(t('appliedBoundedPreset'), 'dirty');
      }
      if (button && button.id === 'retryModePresetInfinite') {
        event.preventDefault();
        byId('cfgRetryInfiniteOnError').checked = true;
        clearValidationState();
        updateSettingsSummary();
        setConfigHintState(t('appliedInfinitePreset'), 'dirty');
      }
      if (button && button.id === 'addBridgeRule') {
        event.preventDefault();
        const current = collectBridgeRules();
        renderBridgeRules([...current, { from: '', to: '' }]);
        clearValidationState();
        updateSettingsSummary();
      }
      if (button && button.id === 'addIntercept') {
        event.preventDefault();
        const current = collectIntercepts();
        renderIntercepts([...current, { name: '', enabled: true, action: 'fail', paths: [], status_codes: [], status_code_min: null, message_keywords: [] }]);
        clearValidationState();
      }
      if (button && button.id === 'addUpstream') {
        event.preventDefault();
        const current = collectUpstreams();
        renderUpstreamsConfig([...current, { name: '', base_url: '', api_key: '', provider_class: 'quota_limited', models: [], headers: {}, weight: 1, timeout_ms: 30000, same_upstream_retries: 0, enabled: true }]);
        clearValidationState();
        updateSettingsSummary();
      }
      if (button && button.classList.contains('intercept-remove')) {
        event.preventDefault();
        const card = button.closest('[data-intercept]');
        if (card) {
          card.remove();
          if (!document.querySelector('[data-intercept]')) {
            byId('interceptList').innerHTML = '<div class="small">' + escapeHTML(t('noInterceptRules')) + '</div>';
          }
          clearValidationState();
        }
      }
      if (button && button.classList.contains('bridge-rule-remove')) {
        event.preventDefault();
        const card = button.closest('[data-bridge-rule]');
        if (card) {
          card.remove();
          if (!document.querySelector('[data-bridge-rule]')) {
            byId('bridgeRuleList').innerHTML = '<div class="small">' + escapeHTML(t('noBridgeRules')) + '</div>';
          }
          clearValidationState();
          updateSettingsSummary();
        }
      }
      if (button && button.classList.contains('upstream-remove')) {
        event.preventDefault();
        const card = button.closest('[data-upstream-config]');
        if (card) {
          card.remove();
          if (!document.querySelector('[data-upstream-config]')) {
            byId('upstreamConfigList').innerHTML = '<div class="small">' + escapeHTML(t('noProviders')) + '</div>';
          }
          updateSettingsSummary();
          clearValidationState();
        }
      }
      if (button && button.classList.contains('upstream-toggle')) {
        event.preventDefault();
        const card = button.closest('[data-upstream-config]');
        if (card) {
          setUpstreamCollapsed(card, !card.classList.contains('collapsed'));
        }
      }
      if (button && button.classList.contains('upstream-test')) {
        event.preventDefault();
        testUpstreamCard(button.closest('[data-upstream-config]'));
      }
      if (button && button.id === 'collapseSections') {
        event.preventDefault();
        topLevelSections().forEach((section) => setSectionCollapsed(section, true));
        updateActiveSettingsNav();
      }
      if (button && button.id === 'expandSections') {
        event.preventDefault();
        topLevelSections().forEach((section) => setSectionCollapsed(section, false));
        updateActiveSettingsNav();
      }
      if (button && button.classList.contains('section-toggle')) {
        event.preventDefault();
        const section = button.closest('.config-section');
        if (section) {
          setSectionCollapsed(section, !section.classList.contains('collapsed'));
          updateActiveSettingsNav();
        }
      }
      if (button && button.id === 'saveConfig') {
        event.preventDefault();
        saveConfig();
      }
      if (button && button.id === 'reloadConfig') {
        event.preventDefault();
        loadConfig();
      }
      if (button && button.id === 'exportConfig') {
        event.preventDefault();
        exportConfig();
      }
      if (button && button.id === 'rollbackConfig') {
        event.preventDefault();
        rollbackConfig();
      }
      if (button && button.classList.contains('history-rollback')) {
        event.preventDefault();
        rollbackConfig(button.getAttribute('data-version-id') || '');
      }
      if (button && button.classList.contains('history-preview')) {
        event.preventDefault();
        loadConfigDiff(button.getAttribute('data-version-id') || '');
      }
    });
    document.addEventListener('input', (event) => {
      if (event.target && event.target.id === 'configSearch') {
        applyConfigSearch();
        return;
      }
      if (event.target && event.target.id === 'providerClassFilter') {
        applyProviderClassFilter();
        updateSettingsSummary();
        return;
      }
      if (event.target && event.target.id === 'cfgAdminLanguage') {
        setLocale(event.target.value || currentLocale);
        updateSettingsSummary();
        setConfigHintState(t('unsavedChanges'), 'dirty');
        return;
      }
      if (event.target && event.target.closest('.config-panel')) {
        updateSettingsSummary();
        clearValidationState();
        setConfigHintState(t('unsavedChanges'), 'dirty');
      }
    });
    document.addEventListener('change', (event) => {
      if (event.target && event.target.id === 'providerClassFilter') {
        applyProviderClassFilter();
        updateSettingsSummary();
        return;
      }
      if (event.target && event.target.id === 'cfgAdminLanguage') {
        setLocale(event.target.value || currentLocale);
        updateSettingsSummary();
        setConfigHintState(t('unsavedChanges'), 'dirty');
        return;
      }
      if (event.target && event.target.closest('.config-panel')) {
        updateSettingsSummary();
        clearValidationState();
        setConfigHintState(t('unsavedChanges'), 'dirty');
      }
    });
    applyStaticLocale();
    ensureSectionControls();
    revealSettingsIfHash();
    window.addEventListener('scroll', updateActiveSettingsNav, { passive: true });
    window.addEventListener('hashchange', updateActiveSettingsNav);
    window.addEventListener('scroll', updateActiveTopnav, { passive: true });
    window.addEventListener('hashchange', updateActiveTopnav);

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
          { name: localeText('平均延迟 (ms)', 'Avg Latency (ms)'), values: latVals, labels, color: CHART_COLORS[3] },
          { name: localeText('成功率 %', 'Success %'), values: srVals, labels, color: CHART_COLORS[0] },
        ], { area: false, fmtVal: (v) => v.toFixed(1) });
        renderLegend('legendLatency', [[localeText('平均延迟 (ms)', 'Avg Latency (ms)'), CHART_COLORS[3]], [localeText('成功率 %', 'Success %'), CHART_COLORS[0]]]);

        // Token by upstream stacked bar
        const allUpstreams = new Set();
        byUp.forEach(b => { for (const k of Object.keys(b.upstreams || {})) allUpstreams.add(k); });
        const upNames = Array.from(allUpstreams);
        const stackData = byUp.map(b => b.upstreams || {});
        drawStackedBarChart('chartTokens', 'tipTokens', labels, stackData, upNames);
        renderLegend('legendTokens', upNames.map((n, i) => [n, CHART_COLORS[i % CHART_COLORS.length]]));

        // Success / failure stacked bar
        const sfLabels = labels;
        const successLabel = localeText('成功', 'Success');
        const failureLabel = localeText('失败', 'Failure');
        const sfStack = buckets.map(b => ({ [successLabel]: b.successes, [failureLabel]: b.failures }));
        drawStackedBarChart('chartSuccess', 'tipSuccess', sfLabels, sfStack, [successLabel, failureLabel]);
        renderLegend('legendSuccess', [[successLabel, CHART_COLORS[0]], [failureLabel, CHART_COLORS[3]]]);
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
      const pricingModels = (pricing.models || []).slice().sort((a, b) => (b.cost?.total_usd || 0) - (a.cost?.total_usd || 0));
      const recentRequests = (data.telemetry.requests || []).slice(0, 24);
      const recentErrors = data.telemetry.errors || [];
      const upstreamStatuses = data.upstreams || {};
      const upstreamUsageEntries = Object.entries(data.telemetry.by_upstream || {});
      const runtime = data.runtime || {};
      const recoveryMode = runtime.retry_infinite_on_error ? localeText('无限恢复', 'always recover') : localeText('有界', 'bounded');
      const enabledRuntimeUpstreams = Number(runtime.enabled_upstreams || 0);
      const totalRuntimeUpstreams = Number(runtime.total_upstreams || 0);
      const runtimeHealthPath = String(runtime.health_path || '').trim() || '/v1/models';
      const runtimeStrategy = String(runtime.router_strategy || data.router_strategy || 'health_weighted_rr');

      document.getElementById('generatedAt').textContent = localeText('更新于 ', 'Updated ') + new Date(data.generated_at).toLocaleString(currentLocale === 'en' ? 'en-US' : 'zh-CN');
      document.getElementById('pricingSource').innerHTML = pricing.source_url
        ? localeText('官方价格：', 'Official pricing: ') + '<a href="' + pricing.source_url + '" target="_blank" rel="noreferrer">OpenAI</a>' + (pricing.updated_at ? ' · ' + relativeTime(pricing.updated_at) : '')
        : t('officialPriceUnavailable');
      const overallCacheRate = cacheHitRate(summary);
      const dayCacheRate = cacheHitRate(dayWindow);
      const avgCostPerRequest = (summary.total_requests || 0) > 0 ? (pricingSummary.total_usd || 0) / summary.total_requests : 0;
      const degradedUpstreams = Object.values(upstreamStatuses).filter((status) => status && !status.healthy).length;
      const recent503 = recentRequests.filter((item) => Number(item.status_code || 0) === 503).length;
      const heroPriority = document.getElementById('heroPriority');
      if (heroPriority) {
        heroPriority.innerHTML = [
          surfaceCard(localeText('1 分钟吞吐', '1m Throughput'), fmtRate(oneMinute.rpm) + ' RPM', fmtRate(oneMinute.tpm) + ' TPM', 'tone-good'),
          surfaceCard(localeText('1 分钟延迟', '1m Latency'), fmtMs(oneMinute.avg_latency_ms || 0), fmtPct(oneMinute.success_rate || 0) + localeText(' 成功率', ' success'), (oneMinute.avg_latency_ms || 0) > 4000 ? 'tone-warn' : ''),
          surfaceCard(localeText('退化上游', 'Degraded Upstreams'), fmt.format(degradedUpstreams), fmt.format(Math.max(Object.keys(upstreamStatuses).length - degradedUpstreams, 0)) + localeText(' 条健康', ' healthy'), degradedUpstreams ? 'tone-danger' : 'tone-good'),
          surfaceCard(localeText('最近 503', 'Recent 503'), fmt.format(recent503), recent503 ? localeText('最近样本里出现了 503 压力。', 'Latest sample contains 503 pressure') : localeText('最近样本里没有 503。', 'No recent 503 in latest sample'), recent503 ? 'tone-warn' : 'tone-good'),
        ].join('');
      }
      document.getElementById('runtimeTopline').innerHTML = [
        surfaceCard(
          localeText('恢复模式', 'Recovery Mode'),
          recoveryMode,
          runtime.retry_infinite_on_error
            ? localeText('传输、状态码和拦截到的响应体错误都会持续重试，直到调用方取消。', 'Retries transport, status, and intercepted body failures until the caller cancels.')
            : localeText('只有命中可重试失败时才会继续，达到边界后退出。', 'Retries only matched retryable failures before exiting.'),
          runtime.retry_infinite_on_error ? 'tone-good' : 'tone-warn'
        ),
        surfaceCard(
          localeText('探活状态', 'Health Probe'),
          runtime.health_enabled ? localeText('已启用', 'armed') : t('disabled'),
          runtime.health_enabled ? runtimeHealthPath : localeText('当前未启用主动探活。', 'No active health polling'),
          runtime.health_enabled ? 'tone-good' : 'tone-warn'
        ),
      ].join('');
      document.getElementById('runtimeMetrics').innerHTML = [
        [localeText('路由策略', 'Router Strategy'), runtimeStrategy, fmt.format(runtime.max_retries || 0) + localeText(' 次最大重试', ' max retries configured')],
        [localeText('恢复上限', 'Recovery Ceiling'), runtime.retry_infinite_on_error ? localeText('由客户端取消', 'client cancel') : (fmt.format(runtime.max_retries || 0) + localeText(' 次重试', ' retries')), runtime.retry_infinite_on_error ? localeText('无限模式会忽略最大重试上限。', 'Infinite mode ignores the max retry ceiling.') : localeText('当前为有界故障转移窗口。', 'Bounded failover window.')],
        [localeText('服务商', 'Providers'), fmt.format(enabledRuntimeUpstreams) + ' ' + localeText('已启用', 'enabled'), fmt.format(Math.max(totalRuntimeUpstreams - enabledRuntimeUpstreams, 0)) + ' ' + localeText('已停用', 'disabled') + ' · ' + (runtime.bridge_enabled ? localeText('桥接开启', 'bridge on') : localeText('桥接关闭', 'bridge off'))],
      ].map(([k, v, small]) => '<div class="metric"><div class="k">' + k + '</div><div class="v mono">' + v + '</div><div class="small">' + small + '</div></div>').join('');
      document.getElementById('performanceMeta').innerHTML = [
        miniChip('1m RPM', fmtRate(oneMinute.rpm), 'accent'),
        miniChip(localeText('1 分钟成功率', '1m Success'), fmtPct(oneMinute.success_rate || 0), (oneMinute.success_rate || 0) < 95 ? 'warn' : 'accent'),
        miniChip(localeText('1 分钟延迟', '1m Latency'), fmtMs(oneMinute.avg_latency_ms || 0), oneMinute.avg_latency_ms > 4000 ? 'warn' : ''),
      ].join('');

      document.getElementById('metrics').innerHTML = [
        [localeText('1 分钟吞吐', '1m Throughput'), fmtRate(oneMinute.rpm) + ' RPM', fmtRate(oneMinute.tpm) + ' TPM'],
        [localeText('1 分钟成功率', '1m Success'), fmtPct(oneMinute.success_rate), fmtMs(oneMinute.avg_latency_ms)],
        [localeText('5 分钟吞吐', '5m Throughput'), fmtRate(fiveMinute.rpm) + ' RPM', fmtRate(fiveMinute.tpm) + ' TPM'],
        [localeText('5 分钟成功率', '5m Success'), fmtPct(fiveMinute.success_rate), fmtMs(fiveMinute.avg_latency_ms)],
        [localeText('1 分钟请求数', '1m Requests'), fmt.format(oneMinute.requests || 0), fmt.format(oneMinute.failures || 0) + localeText(' 次失败', ' failures')],
        [localeText('5 分钟请求数', '5m Requests'), fmt.format(fiveMinute.requests || 0), fmt.format(fiveMinute.failures || 0) + localeText(' 次失败', ' failures')],
        [localeText('总请求数', 'Total Requests'), fmt.format(summary.total_requests || 0), fmt.format(summary.successes || 0) + localeText(' 成功 / ', ' success / ') + fmt.format(summary.failures || 0) + localeText(' 失败', ' fail')],
        [localeText('总 Token', 'Total Tokens'), fmt.format(summary.total_tokens || 0), fmt.format(summary.prompt_tokens || 0) + localeText(' 提示词 / ', ' prompt / ') + fmt.format(summary.completion_tokens || 0) + localeText(' 补全', ' completion')],
      ].map(([k, v, small]) => '<div class="metric"><div class="k">' + k + '</div><div class="v mono">' + v + '</div><div class="small">' + small + '</div></div>').join('');

      document.getElementById('costMetrics').innerHTML = [
        [localeText('估算成本', 'Estimated Cost'), fmtMoney(pricingSummary.total_usd || 0), fmtMoney(pricingSummary.prompt_usd || 0) + localeText(' 输入 / ', ' input / ') + fmtMoney(pricingSummary.completion_usd || 0) + localeText(' 输出', ' output')],
        [localeText('已定价模型', 'Priced Models'), fmt.format(pricingSummary.priced_models || 0), fmt.format(pricingSummary.unpriced_models || 0) + localeText(' 个未定价', ' unpriced')],
        [localeText('缓存提示词', 'Cached Prompt'), fmt.format(pricingSummary.cached_prompt_tokens || 0), fmtMoney(pricingSummary.cache_savings_usd || 0) + localeText(' 已节省', ' saved')],
        [localeText('缓存命中', 'Cache Hit'), overallCacheRate === null ? localeText('无', 'n/a') : fmtPct(overallCacheRate), fmt.format(summary.cached_prompt_tokens || 0) + ' / ' + fmt.format(summary.prompt_tokens || 0) + localeText(' 提示词 token', ' prompt tokens')],
        [localeText('1 小时缓存命中', '1h Cache Hit'), cacheHitRate(oneHour) === null ? localeText('无', 'n/a') : fmtPct(cacheHitRate(oneHour)), cacheTrendDetail(oneHour)],
        [localeText('24 小时缓存命中', '24h Cache Hit'), cacheHitRate(dayWindow) === null ? localeText('无', 'n/a') : fmtPct(cacheHitRate(dayWindow)), cacheTrendDetail(dayWindow)],
      ].map(([k, v, small]) => '<div class="metric"><div class="k">' + k + '</div><div class="v mono">' + v + '</div><div class="small">' + small + '</div></div>').join('');
      document.getElementById('costMeta').innerHTML = [
        miniChip(localeText('总额', 'Total'), compactUsd(pricingSummary.total_usd || 0), 'accent'),
        miniChip(localeText('已节省', 'Saved'), compactUsd(pricingSummary.cache_savings_usd || 0), pricingSummary.cache_savings_usd > 0 ? 'accent' : ''),
        miniChip('Avg / 1k', compactUsd(avgCostPerRequest * 1000)),
      ].join('');

      const topCostModel = pricingModels.slice().sort((a, b) => (b.cost?.total_usd || 0) - (a.cost?.total_usd || 0))[0];
      const topEconomics = pricingModels.slice().sort((a, b) => (b.cost?.total_usd || 0) - (a.cost?.total_usd || 0)).slice(0, 3);
      document.getElementById('economicsMeta').innerHTML = [
        miniChip(localeText('已定价', 'Priced'), fmt.format(pricingSummary.priced_models || 0), 'accent'),
        miniChip(localeText('未定价', 'Unpriced'), fmt.format(pricingSummary.unpriced_models || 0), pricingSummary.unpriced_models ? 'warn' : ''),
        miniChip(localeText('最高花费', 'Top Spend'), topCostModel ? ((topCostModel.display_model || '-') + ' · ' + compactUsd(topCostModel.cost?.total_usd || 0)) : localeText('无', 'n/a')),
      ].join('');
      document.getElementById('economicsTopline').innerHTML = topEconomics.length
        ? topEconomics.map((item, idx) => surfaceCard(
            localeText('重点模型 ', 'Top Model ') + (idx + 1),
            item.display_model || '-',
            compactUsd(item.cost?.total_usd || 0) + ' · ' + fmt.format(item.usage?.total_tokens || 0) + localeText(' token', ' tokens')
          )).join('')
        : surfaceCard(localeText('重点模型 1', 'Top Model 1'), localeText('无', 'n/a'), localeText('还没有已定价用量。', 'No priced usage yet'))
          + surfaceCard(localeText('重点模型 2', 'Top Model 2'), localeText('无', 'n/a'), localeText('还没有已定价用量。', 'No priced usage yet'))
          + surfaceCard(localeText('重点模型 3', 'Top Model 3'), localeText('无', 'n/a'), localeText('还没有已定价用量。', 'No priced usage yet'));

      const unhealthyCount = degradedUpstreams;
      const healthEntries = Object.entries(upstreamStatuses);
      const mostFailedUpstream = healthEntries
        .slice()
        .sort((a, b) => ((b[1] && b[1].consecutive_retryable_failures) || 0) - ((a[1] && a[1].consecutive_retryable_failures) || 0))[0];
      const slowestUpstream = healthEntries
        .slice()
        .sort((a, b) => ((b[1] && b[1].last_latency) || 0) - ((a[1] && a[1].last_latency) || 0))[0];
      document.getElementById('upstreamMeta').innerHTML = [
        miniChip(localeText('总数', 'Total'), fmt.format(Object.keys(upstreamStatuses).length), 'accent'),
        miniChip(localeText('退化', 'Degraded'), fmt.format(unhealthyCount), unhealthyCount ? 'danger' : ''),
        miniChip(localeText('窗口', 'Window'), localeText('探活快照', 'health snapshot')),
      ].join('');
      document.getElementById('upstreamTopline').innerHTML = [
        surfaceCard(localeText('退化路由', 'Degraded Routes'), fmt.format(unhealthyCount), fmt.format(healthEntries.length - unhealthyCount) + localeText(' 条健康', ' healthy')),
        surfaceCard(localeText('失败最多', 'Highest Failures'), mostFailedUpstream ? mostFailedUpstream[0] : localeText('无', 'n/a'), mostFailedUpstream ? (fmt.format(mostFailedUpstream[1].consecutive_retryable_failures || 0) + localeText(' 次可重试失败', ' retryable fails')) : localeText('暂无上游探活数据。', 'No upstream telemetry')),
        surfaceCard(localeText('最慢探活', 'Slowest Probe'), slowestUpstream ? slowestUpstream[0] : localeText('无', 'n/a'), slowestUpstream && slowestUpstream[1].last_latency ? fmtMs((slowestUpstream[1].last_latency || 0) / 1000000) : localeText('暂无延迟样本。', 'No latency sample')),
      ].join('');

      document.getElementById('upstreams').innerHTML = renderUpstreamHealth(healthEntries);

      const modelRows = pricingModels
        .map((item) => [
          stackCell(
            '<strong>' + escapeHTML(item.display_model || '-') + '</strong>',
            (item.pricing_model && item.pricing_model !== item.display_model ? localeText('按 ' + escapeHTML(item.pricing_model) + ' 计价 · ', 'priced as ' + escapeHTML(item.pricing_model) + ' · ') : '') + (item.pricing ? ('$' + item.pricing.input_per_1m_usd + ' / $' + item.pricing.output_per_1m_usd + ' ' + t('perMillion')) : t('officialPriceUnavailable'))
          ),
          promptUsageCell(item.usage),
          cacheRateCell(item.usage),
          fmt.format(item.usage.completion_tokens || 0),
          totalUsageCell(item.usage),
          fmtMoney(item.cost.total_usd || 0),
        ]);
      document.getElementById('byModel').innerHTML = table([localeText('模型', 'Model'), localeText('提示词', 'Prompt'), localeText('缓存命中', 'Cache Hit'), localeText('补全', 'Completion'), localeText('总量', 'Total'), 'USD'], modelRows, 'table-models');

      const topUsageUpstream = upstreamUsageEntries.slice().sort((a, b) => ((b[1] && b[1].total_tokens) || 0) - ((a[1] && a[1].total_tokens) || 0))[0];
      document.getElementById('usageMeta').innerHTML = [
        miniChip(localeText('上游', 'Upstreams'), fmt.format(upstreamUsageEntries.length), 'accent'),
        miniChip(localeText('最高吞吐', 'Top Volume'), topUsageUpstream ? (topUsageUpstream[0] + ' · ' + fmt.format(topUsageUpstream[1].total_tokens || 0)) : localeText('无', 'n/a')),
      ].join('');
      const topUsageEntries = upstreamUsageEntries
        .slice()
        .sort((a, b) => ((b[1] && b[1].total_tokens) || 0) - ((a[1] && a[1].total_tokens) || 0))
        .slice(0, 3);
      document.getElementById('usageTopline').innerHTML = topUsageEntries.length
        ? topUsageEntries.map((entry, idx) => surfaceCard(
            localeText('重点上游 ', 'Top Upstream ') + (idx + 1),
            entry[0],
            fmt.format(entry[1].total_tokens || 0) + localeText(' 总量 · ', ' total · ') + fmt.format(entry[1].completion_tokens || 0) + localeText(' 补全', ' completion')
          )).join('')
        : surfaceCard(localeText('重点上游 1', 'Top Upstream 1'), localeText('无', 'n/a'), localeText('还没有用量数据。', 'No usage data'))
          + surfaceCard(localeText('重点上游 2', 'Top Upstream 2'), localeText('无', 'n/a'), localeText('还没有用量数据。', 'No usage data'))
          + surfaceCard(localeText('重点上游 3', 'Top Upstream 3'), localeText('无', 'n/a'), localeText('还没有用量数据。', 'No usage data'));

      const upstreamUsageRows = upstreamUsageEntries
        .slice()
        .sort((a, b) => ((b[1] && b[1].total_tokens) || 0) - ((a[1] && a[1].total_tokens) || 0))
        .map(([name, usage]) => [
          stackCell('<strong>' + escapeHTML(name) + '</strong>', fmt.format(usage.total_tokens || 0) + localeText(' 总量', ' total')),
          promptUsageCell(usage),
          cacheRateCell(usage),
          stackCell(fmt.format(usage.completion_tokens || 0), localeText('补全 token', 'completion tokens')),
          totalUsageCell(usage),
        ]);
      document.getElementById('byUpstream').innerHTML = table([localeText('上游', 'Upstream'), localeText('提示词', 'Prompt'), localeText('缓存命中', 'Cache Hit'), localeText('补全', 'Completion'), localeText('总量', 'Total')], upstreamUsageRows, 'table-usage');

      const cacheRanking = data.telemetry.cache_hit_ranking || [];
      const topErrorUpstream = aggregateBy(recentErrors, (item) => item.upstream || '-')[0];
      const topErrorStatus = aggregateBy(recentErrors, (item) => item.status_code || '-')[0];
      const topErrorModel = aggregateBy(recentErrors, (item) => item.model || '-')[0];
      document.getElementById('cacheMeta').innerHTML = [
        miniChip(localeText('24 小时命中', '24h Hit'), dayCacheRate === null ? localeText('无', 'n/a') : fmtPct(dayCacheRate), dayCacheRate !== null && dayCacheRate >= 50 ? 'accent' : ''),
        miniChip(localeText('已节省', 'Saved'), compactUsd(pricingSummary.cache_savings_usd || 0), 'accent'),
        miniChip(localeText('领先者', 'Leaders'), fmt.format(cacheRanking.length)),
      ].join('');
      document.getElementById('cacheTopline').innerHTML = cacheRanking.slice(0, 3).map((item, idx) => surfaceCard(
        localeText('缓存领先 ', 'Cache Leader ') + (idx + 1),
        item.upstream || '-',
        (cacheHitRate(item.usage) === null ? localeText('无', 'n/a') : fmtPct(cacheHitRate(item.usage))) + localeText(' 命中 · ', ' hit · ') + fmt.format(item.requests || 0) + localeText(' 次请求', ' req')
      )).join('') || surfaceCard(localeText('缓存领先 1', 'Cache Leader 1'), localeText('无', 'n/a'), localeText('还没有缓存排行。', 'No cache ranking'))
        + surfaceCard(localeText('缓存领先 2', 'Cache Leader 2'), localeText('无', 'n/a'), localeText('还没有缓存排行。', 'No cache ranking'))
        + surfaceCard(localeText('缓存领先 3', 'Cache Leader 3'), localeText('无', 'n/a'), localeText('还没有缓存排行。', 'No cache ranking'));

      const cacheRankingRows = cacheRanking.map((item, idx) => [
        stackCell('<strong>' + escapeHTML((idx + 1) + '. ' + (item.upstream || '-')) + '</strong>', fmt.format(item.requests || 0) + localeText(' 次请求', ' requests')),
        cacheRateCell(item.usage),
        stackCell(fmt.format((item.usage && item.usage.cached_prompt_tokens) || 0), localeText('缓存提示词', 'cached prompt')),
        stackCell(fmt.format((item.usage && item.usage.prompt_tokens) || 0), localeText('提示词总量', 'prompt total')),
        stackCell(fmt.format(item.requests || 0), localeText('排行窗口', 'ranking window')),
      ]);
      document.getElementById('cacheRanking').innerHTML = table([localeText('上游', 'Upstream'), localeText('缓存命中', 'Cache Hit'), localeText('缓存量', 'Cached'), localeText('提示词', 'Prompt'), localeText('请求数', 'Requests')], cacheRankingRows, 'table-cache');

      const leadingError = recentErrors[0];
      document.getElementById('errorsMeta').innerHTML = [
        miniChip(localeText('行数', 'Rows'), fmt.format(recentErrors.length), recentErrors.length ? 'danger' : 'accent'),
        miniChip(localeText('主要上游', 'Top Upstream'), leadingError?.upstream || localeText('无', 'n/a')),
        miniChip(localeText('最新', 'Latest'), leadingError?.status_code ? (localeText('状态 ', 'status ') + leadingError.status_code) : localeText('干净', 'clean'), leadingError ? 'warn' : 'accent'),
      ].join('');
      document.getElementById('errorsTopline').innerHTML = [
        surfaceCard(localeText('主导上游', 'Dominant Upstream'), topErrorUpstream ? topErrorUpstream[0] : localeText('无', 'n/a'), topErrorUpstream ? (fmt.format(topErrorUpstream[1]) + localeText(' 条错误行', ' error rows')) : localeText('最近没有错误。', 'No recent errors')),
        surfaceCard(localeText('主导状态码', 'Dominant Status'), topErrorStatus ? String(topErrorStatus[0]) : localeText('无', 'n/a'), topErrorStatus ? (fmt.format(topErrorStatus[1]) + localeText(' 行样本', ' rows in sample')) : localeText('最近没有错误。', 'No recent errors')),
        surfaceCard(localeText('主导模型', 'Dominant Model'), topErrorModel ? topErrorModel[0] : localeText('无', 'n/a'), topErrorModel ? (fmt.format(topErrorModel[1]) + localeText(' 条错误行', ' error rows')) : localeText('最近没有错误。', 'No recent errors')),
      ].join('');
      document.getElementById('errors').innerHTML = errorFeed(recentErrors);

      const avgAttempts = recentRequests.length
        ? (recentRequests.reduce((sum, item) => sum + Number(item.attempts || 0), 0) / recentRequests.length)
        : 0;
      const hottestPath = aggregateBy(recentRequests, (item) => item.path)[0];
      const busiestUpstream = aggregateBy(recentRequests, (item) => item.upstream || '-')[0];
      const hottestModel = aggregateBy(recentRequests, (item) => item.model || item.requested_model || '-')[0];
      document.getElementById('requestsMeta').innerHTML = [
        miniChip(localeText('行数', 'Rows'), fmt.format(recentRequests.length), 'accent'),
        miniChip('503', fmt.format(recent503), recent503 ? 'danger' : ''),
        miniChip(localeText('平均尝试', 'Avg Attempts'), recentRequests.length ? avgAttempts.toFixed(1) : localeText('无', 'n/a')),
      ].join('');
      document.getElementById('requestsTopline').innerHTML = [
        surfaceCard(localeText('最热路径', 'Hottest Path'), hottestPath ? hottestPath[0] : localeText('无', 'n/a'), hottestPath ? (fmt.format(hottestPath[1]) + localeText(' 行当前样本', ' rows in current sample')) : localeText('最近没有请求。', 'No recent requests')),
        surfaceCard(localeText('最忙上游', 'Busiest Upstream'), busiestUpstream ? busiestUpstream[0] : localeText('无', 'n/a'), busiestUpstream ? (fmt.format(busiestUpstream[1]) + localeText(' 次路由请求', ' routed requests')) : localeText('当前没有上游流量。', 'No upstream traffic')),
        surfaceCard(localeText('最热模型', 'Hottest Model'), hottestModel ? hottestModel[0] : localeText('无', 'n/a'), hottestModel ? (fmt.format(hottestModel[1]) + localeText(' 行当前样本', ' rows in current sample')) : localeText('当前没有模型流量。', 'No model traffic')),
      ].join('');
      document.getElementById('requests').innerHTML = table(
        [localeText('时间', 'Time'), localeText('路径与路由', 'Route + Path'), localeText('模型流向', 'Model Flow'), localeText('上游', 'Upstream'), localeText('状态 / 尝试', 'Status / Attempts'), localeText('延迟 / 缓存', 'Latency + Cache'), localeText('Token', 'Tokens'), 'USD'],
        recentRequests.map(item => [
          stackCell(escapeHTML(relativeTime(item.timestamp)), escapeHTML(item.request_id || '-')),
          stackCell(
            escapeHTML(item.path || '-'),
            escapeHTML(routeModeLabel(item.route_mode)),
            '<div class="cell-tags"><span class="tag accent">' + escapeHTML(routeModeLabel(item.route_mode)) + '</span></div>'
          ),
          stackCell(modelFlow(item), item.requested_model && item.requested_model !== item.model ? localeText('已桥接', 'bridge applied') : localeText('直接模型', 'direct model')),
          stackCell(escapeHTML(item.upstream || '-'), item.error_message ? escapeHTML(item.error_message) : ''),
          stackCell(statusChip(item.status_code), localeText('尝试 ', 'attempt ') + escapeHTML(item.attempts || 0)),
          stackCell(fmtMs(item.duration_ms || 0), localeText('缓存 ', 'cache ') + (cacheHitRate(item.usage) === null ? localeText('无', 'n/a') : fmtPct(cacheHitRate(item.usage)))),
          stackCell(fmt.format((item.usage && item.usage.total_tokens) || 0), localeText('缓存 ', 'cached ') + fmt.format((item.usage && item.usage.cached_prompt_tokens) || 0)),
          stackCell(estimateRequestCost(item, pricing) || ('<span class="small">' + escapeHTML(localeText('无', 'n/a')) + '</span>'), fmt.format((item.usage && item.usage.completion_tokens) || 0) + localeText(' 补全', ' completion')),
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
      updateActiveTopnav();
      setInterval(load, 5000);
      setInterval(loadCharts, 15000);
    }
  </script>
</body>
</html>`

func adminHTMLLang(language string) string {
	if config.NormalizeAdminLanguage(language) == config.AdminLanguageEnglish {
		return "en"
	}
	return "zh-CN"
}

func renderAdminHTML(settingsView bool, language string) string {
	language = config.NormalizeAdminLanguage(language)
	isEnglish := language == config.AdminLanguageEnglish
	bodyClass := ""
	topnavLinks := strings.Join([]string{
		`<a href="#performance" data-topnav-target="performance">` + map[bool]string{true: "Performance", false: "性能"}[isEnglish] + `</a>`,
		`<a href="#economics" data-topnav-target="economics">` + map[bool]string{true: "Economics", false: "成本"}[isEnglish] + `</a>`,
		`<a href="#upstreams-card" data-topnav-target="upstreams-card">` + map[bool]string{true: "Upstreams", false: "上游"}[isEnglish] + `</a>`,
		`<a href="#requests-card" data-topnav-target="requests-card">` + map[bool]string{true: "Requests", false: "请求"}[isEnglish] + `</a>`,
		`<a href="/admin/settings">` + map[bool]string{true: "Settings", false: "设置"}[isEnglish] + `</a>`,
	}, "")
	heroEyebrow := map[bool]string{true: "AI Gateway Admin", false: "AI 模型网关管理台"}[isEnglish]
	heroTitle := map[bool]string{true: "Ops, Cost, Throughput.", false: "运维、成本、吞吐。"}[isEnglish]
	heroSub := map[bool]string{true: "Check throughput, latency, errors, and upstream health first, then drill into cost and cache.", false: "先看吞吐、延迟、错误和上游健康，再往下追成本与缓存。"}[isEnglish]
	heroMetaPrimary := `<div class="pill" id="generatedAt">` + map[bool]string{true: "Loading", false: "加载中"}[isEnglish] + `</div>`
	heroMetaSecondary := `<div class="pill" id="pricingSource">` + map[bool]string{true: "Pricing source", false: "价格来源"}[isEnglish] + `</div>`
	heroMetaTertiary := ``
	heroAside := `<div class="hero-priority-grid" id="heroPriority"></div>`
	if settingsView {
		bodyClass = "page-settings"
		topnavLinks = strings.Join([]string{
			`<a href="/admin">` + map[bool]string{true: "Overview", false: "总览"}[isEnglish] + `</a>`,
		}, "")
		heroEyebrow = map[bool]string{true: "Configuration Center", false: "配置中心"}[isEnglish]
		heroTitle = map[bool]string{true: "Runtime Routing, Health, Providers.", false: "运行路由、探活、服务商。"}[isEnglish]
		heroSub = map[bool]string{true: "Manage probes, bridge, recovery, and providers in one place instead of jumping across surfaces.", false: "集中维护探活、桥接、恢复和服务商，不再在多个面板里来回切换。"}[isEnglish]
		heroMetaPrimary = ``
		heroMetaSecondary = ``
		heroMetaTertiary = ``
		heroAside = ``
	}
	pageTitle := "AI Gateway Admin"
	if settingsView {
		pageTitle = "AI Gateway Settings"
	}
	if language == config.AdminLanguageChinese {
		pageTitle = "AI 模型网关管理台"
		if settingsView {
			pageTitle = "AI 模型网关设置"
		}
	}
	return strings.NewReplacer(
		"{{HTML_LANG}}", adminHTMLLang(language),
		"{{PAGE_TITLE}}", pageTitle,
		"{{BOOTSTRAP_LANGUAGE}}", language,
		"{{BODY_CLASS}}", bodyClass,
		"{{TOPNAV_LINKS}}", topnavLinks,
		"{{HERO_EYEBROW}}", heroEyebrow,
		"{{HERO_TITLE}}", heroTitle,
		"{{HERO_SUB}}", heroSub,
		"{{HERO_META_PRIMARY}}", heroMetaPrimary,
		"{{HERO_META_SECONDARY}}", heroMetaSecondary,
		"{{HERO_META_TERTIARY}}", heroMetaTertiary,
		"{{HERO_ASIDE}}", heroAside,
	).Replace(adminHTMLTemplate)
}

func adminPage(settingsView bool, manager *router.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		language := config.AdminLanguageChinese
		if manager != nil {
			language = manager.CurrentConfig().Admin.Language
		}
		_, _ = w.Write([]byte(renderAdminHTML(settingsView, language)))
	}
}

func adminFavicon() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		_, _ = w.Write([]byte(adminIconSVG))
	}
}
