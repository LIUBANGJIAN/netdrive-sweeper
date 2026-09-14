package main

import "html/template"

var pageTpl = template.Must(template.New("page").Parse(pageHTML))

const pageHTML = `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<link rel="icon" href="data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24' fill='none' stroke='%2358a6ff' stroke-width='2' stroke-linecap='round' stroke-linejoin='round'%3E%3Cpath d='M3 6h18'/%3E%3Cpath d='M8 6V4h8v2'/%3E%3Cpath d='M6 6l1 14h10l1-14'/%3E%3C/svg%3E">
<title>{{.Title}}</title>
<style>
:root{--bg:#0d1117;--panel:#161b22;--card:#21262d;--line:#30363d;--text:#e6edf3;--text-dim:#c9d1d9;--muted:#8b949e;--log-bg:#010409;
--blue:#58a6ff;--green:#3fb950;--red:#f85149;--orange:#d29922;--purple:#a371f7;
--danger:#f85149;--danger-bg:#da3633;--danger-text:#ff7b72;--success:#3fb950;--success-bg:#238636;--success-hover:#2ea043;--success-text:#6ee7b7;
--warning:#d29922;--warning-bg:#2d1a05;--warning-text:#f0c674;--info:#58a6ff;--info-bg:#0969da;--info-hover:#0d6eaf;
--ok-bg:#0e4429;--ok-text:#6ee7b7;--bad-bg:#490202;--bad-text:#ff7b72;
--r-sm:6px;--r-md:8px;--r-lg:12px;--r-xl:16px;--s1:4px;--s2:8px;--s3:12px;--s4:16px;--s5:24px;--s6:32px;
--z-header:100;--z-busy:200;--z-modal:300;--z-toast:400}
*{box-sizing:border-box}
body{margin:0;background:var(--bg);font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;color:var(--text);font-size:14px;min-height:100vh}
h1,h2,h3{margin:0}
a{color:var(--blue)}
.mono{font-family:ui-monospace,Menlo,Consolas,monospace}
.hidden{display:none!important}
.danger-text{color:var(--danger-text)}
.muted{color:var(--muted)}

/* ---------- head (sticky) ---------- */
.head{position:sticky;top:0;z-index:var(--z-header);background:var(--panel);border-bottom:1px solid var(--line)}
.topbar{padding:10px 18px;display:flex;flex-direction:column;gap:8px}
.tb-row{display:flex;align-items:center;gap:12px;flex-wrap:wrap}
.brand{display:flex;align-items:center;gap:8px}
.brand svg{width:18px;height:18px}
.brand h1{font-size:17px;font-weight:700;color:var(--blue)}
.conn{display:flex;align-items:center;gap:6px;font-size:12px;color:var(--muted)}
.dot{width:8px;height:8px;border-radius:999px;background:var(--muted);flex:0 0 auto}
.dot.ok{background:var(--green);box-shadow:0 0 8px rgba(63,185,80,.4)}
.dot.connecting{background:var(--orange)}
.dot.fail{background:var(--red)}
.perms{display:flex;gap:5px;flex-wrap:wrap}
.badge{padding:2px 6px;border-radius:4px;font-size:11px;line-height:1.4}
.badge.ok{background:var(--ok-bg);color:var(--ok-text)}
.badge.bad{background:var(--bad-bg);color:var(--bad-text);cursor:help}
.badge.neutral{background:var(--bg);color:var(--muted)}
.badge.count{background:var(--card);color:var(--muted);border-radius:999px}
.tb-spacer{flex:1 1 auto}
.dirty{font-size:12px;color:var(--warning-text)}
.tb-meta{font-size:12px;color:var(--muted)}
.suggest{color:var(--blue)}
.progress{height:2px;width:100%;background:transparent;overflow:hidden}
.progress.on{background:linear-gradient(90deg,transparent 0,var(--blue) 40%,var(--green) 60%,transparent 100%);background-size:30% 100%;background-repeat:no-repeat;animation:slide 1.1s linear infinite}
@keyframes slide{0%{background-position:-40% 0}100%{background-position:140% 0}}

/* ---------- tabs ---------- */
.tabs{display:flex;gap:4px;padding:0 12px;border-top:1px solid var(--line);overflow-x:auto}
.tab{position:relative;border:0;background:transparent;color:var(--muted);font-size:13px;font-weight:600;padding:10px 12px;cursor:pointer;white-space:nowrap;border-bottom:2px solid transparent}
.tab:hover{color:var(--text)}
.tab.active{color:var(--blue);border-bottom-color:var(--blue)}
.tab .dotmini{display:inline-block;width:6px;height:6px;border-radius:999px;background:var(--warning);margin-left:5px;vertical-align:middle}

/* ---------- layout ---------- */
.main{max-width:1400px;margin:0 auto;padding:var(--s4)}
.grid{display:grid;grid-template-columns:1fr 380px;gap:var(--s4)}
.grid2{display:grid;grid-template-columns:1fr 1fr;gap:var(--s4)}
.col{display:grid;gap:var(--s4);align-content:start}
section.tabpane{display:none}
section.tabpane.active{display:block}
.card{background:var(--panel);border:1px solid var(--line);border-radius:var(--r-xl);padding:18px}
.card h2{font-size:15px;font-weight:600;color:var(--text-dim);margin-bottom:12px;display:flex;align-items:center;gap:8px}
.card h2 .spacer{flex:1}
.card h2 .step{color:var(--muted);font-size:12px}
.sub{color:var(--muted);font-size:12px;margin:-6px 0 12px}

/* ---------- buttons ---------- */
.btn{border:0;border-radius:var(--r-md);padding:9px 12px;font-size:13px;font-weight:600;cursor:pointer;transition:.15s;display:inline-flex;align-items:center;gap:6px;line-height:1}
.btn:focus-visible{outline:2px solid var(--info);outline-offset:2px}
.btn:disabled{opacity:.45;cursor:not-allowed}
.btn-primary{background:var(--success-bg);color:#fff}.btn-primary:hover:not(:disabled){background:var(--success-hover)}
.btn-ok{background:var(--info-bg);color:#fff}.btn-ok:hover:not(:disabled){background:var(--info-hover)}
.btn-danger{background:var(--danger-bg);color:#fff}.btn-danger:hover:not(:disabled){background:var(--danger)}
.btn-ghost{background:var(--bg);border:1px solid var(--line);color:var(--text)}.btn-ghost:hover:not(:disabled){background:var(--card)}
.btn-sm{padding:5px 9px;font-size:12px}
.actions{display:flex;gap:var(--s2);flex-wrap:wrap;margin-top:var(--s3)}
.spinner{width:14px;height:14px;border-radius:999px;border:2px solid rgba(255,255,255,.35);border-top-color:#fff;animation:spin .7s linear infinite;display:inline-block}
.spinner.dark{border-color:rgba(255,255,255,.15);border-top-color:var(--blue)}
@keyframes spin{to{transform:rotate(360deg)}}

/* ---------- forms ---------- */
.formgroup{margin-bottom:var(--s3)}
.formgroup label{display:block;margin-bottom:5px;color:var(--muted);font-size:12px;font-weight:500}
.formgroup input,.formgroup textarea,.formgroup select{width:100%;border:1px solid var(--line);background:var(--bg);color:var(--text);border-radius:var(--r-md);padding:9px 11px;outline:none;font-size:13px;transition:.15s;height:36px}
.formgroup textarea{height:auto;min-height:60px;resize:vertical}
.formgroup input:focus,.formgroup textarea:focus,.formgroup select:focus{border-color:var(--blue);box-shadow:0 0 0 3px rgba(88,166,255,.1)}
.formgroup .help{font-size:11px;color:var(--muted);margin-top:4px}
.formgroup .help.warn{color:var(--warning-text)}
.checks .help.warn{margin-top:6px;padding:8px 12px;font-size:12px;line-height:1.5;color:var(--warning-text);background:var(--warning-bg);border:1px solid var(--warning);border-radius:6px}
.row2{display:grid;grid-template-columns:1fr 1fr;gap:var(--s3)}.row3{display:grid;grid-template-columns:1fr 1fr 1fr;gap:var(--s3)}
.checks{display:flex;gap:var(--s2);flex-wrap:wrap;margin-top:var(--s3)}
.check{display:flex;align-items:center;gap:6px;border:1px solid var(--line);border-radius:var(--r-sm);padding:6px 9px;background:var(--bg);font-size:12px;color:var(--text)}
.check.warn{border-color:var(--danger)}
.check input{width:auto;height:auto}
.inline{display:flex;gap:6px;align-items:center}
.inline input{flex:1}

/* ---------- details (advanced) ---------- */
details.adv{margin-top:var(--s3);border:1px solid var(--line);border-radius:var(--r-md);background:var(--bg)}
details.adv > summary{cursor:pointer;padding:10px 12px;font-size:13px;font-weight:600;color:var(--text-dim);list-style:none}
details.adv > summary::-webkit-details-marker{display:none}
details.adv > summary:before{content:"▸ ";color:var(--muted)}
details.adv[open] > summary:before{content:"▾ "}
details.adv .adv-body{padding:0 12px 12px}

/* ---------- guide / stepper ---------- */
.guide-head{display:flex;align-items:center;gap:8px;margin-bottom:12px}
.guide-head .spacer{flex:1}
.guide-toggle{color:var(--blue);cursor:pointer;font-size:12px}
.stepper{display:flex;gap:6px;align-items:center;flex-wrap:wrap;margin-bottom:var(--s3)}
.stepdot{display:flex;flex-direction:column;align-items:center;gap:4px;min-width:74px}
.stepdot .num{width:28px;height:28px;border-radius:999px;display:flex;align-items:center;justify-content:center;font-size:13px;font-weight:600;background:var(--card);color:var(--muted)}
.stepdot.cur .num{background:var(--info-bg);color:#fff}
.stepdot.done .num{background:var(--success-bg);color:#fff}
.stepdot .lbl{font-size:11px;color:var(--muted);text-align:center}
.stepdot.cur .lbl{color:var(--text)}
.stepline{flex:1;height:2px;background:var(--line);min-width:10px}

/* ---------- stat grid ---------- */
.statgrid{display:grid;grid-template-columns:1fr 1fr;gap:var(--s2)}
.stat{display:flex;justify-content:space-between;padding:10px 12px;background:var(--bg);border-radius:var(--r-md)}
.stat .label{color:var(--muted);font-size:12px}
.stat .value{font-weight:600}

/* ---------- lists ---------- */
.panel{max-height:320px;overflow:auto;border:1px solid var(--line);border-radius:var(--r-md);background:var(--bg)}
.panel::-webkit-scrollbar{width:6px;height:6px}.panel::-webkit-scrollbar-thumb{background:var(--line);border-radius:999px}
.list-empty{padding:var(--s5) 12px;text-align:center;color:var(--muted)}
.list-empty .ico{font-size:32px;display:block;margin-bottom:var(--s2)}
.list-empty .t{color:var(--text-dim);font-size:14px}
.taskitem{display:flex;align-items:center;gap:var(--s2);padding:8px 10px;border-bottom:1px solid #1c2128;font-size:13px}
.taskitem:last-child{border-bottom:0}
.taskitem .path{flex:1;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;color:var(--text-dim)}
.mini{border:1px solid var(--line);background:var(--bg);color:var(--muted);border-radius:var(--r-sm);cursor:pointer;font-size:12px;padding:3px 7px;line-height:1}
.mini:hover:not(:disabled){color:var(--text);background:var(--card)}
.mini:disabled{opacity:.3;cursor:not-allowed}
.mini.del:hover{color:var(--danger-text);border-color:var(--danger)}
.diritem{display:flex;align-items:center;gap:var(--s2);padding:8px 10px;border-bottom:1px solid #1c2128;cursor:pointer}
.diritem:hover{background:var(--card)}
.diritem:last-child{border-bottom:0}
.diritem .name{flex:1;font-size:13px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.diritem .added{color:var(--success-text);font-size:12px}
.diritem input[type=checkbox]{width:auto;height:auto}
.crumbs{display:flex;gap:4px;flex-wrap:wrap;align-items:center;font-size:13px;margin-bottom:var(--s2);color:var(--muted)}
.crumbs .c{cursor:pointer;color:var(--blue)}
.crumbs .c:hover{text-decoration:underline}
.crumbs .cur{color:var(--text);font-weight:600}
.crumbs .sep{color:var(--muted)}

/* ---------- table ---------- */
.tablewrap{border:1px solid var(--line);border-radius:var(--r-md);overflow:auto;background:var(--bg)}
table.tbl{width:100%;border-collapse:collapse;font-size:13px}
table.tbl th{position:sticky;top:0;background:var(--card);color:var(--muted);font-size:12px;font-weight:500;text-align:left;padding:9px 10px;cursor:default;white-space:nowrap;border-bottom:1px solid var(--line)}
table.tbl th.sortable{cursor:pointer}
table.tbl td{padding:7px 10px;border-bottom:1px solid #1c2128;color:var(--text-dim);vertical-align:middle}
table.tbl tr:nth-child(even) td{background:rgba(255,255,255,.02)}
table.tbl tr:hover td{background:var(--card)}
table.tbl .path{max-width:1px;width:100%;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
table.tbl .r{text-align:right}
table.tbl .mark{width:44px;text-align:center}
.markbtn{cursor:pointer;font-size:15px;line-height:1;user-select:none}
.markbtn.on{color:var(--success-text)}
.markbtn.off{color:var(--muted)}
.tblbar{display:flex;gap:var(--s2);flex-wrap:wrap;align-items:center;margin-bottom:var(--s2)}
.tblbar input[type=text],.tblbar select{background:var(--bg);border:1px solid var(--line);color:var(--text);border-radius:var(--r-md);padding:7px 9px;font-size:12px;height:34px}
.tblbar input[type=text]{flex:1;min-width:140px}
.pager{display:flex;gap:var(--s2);align-items:center;justify-content:flex-end;margin-top:var(--s2);font-size:12px;color:var(--muted);flex-wrap:wrap}
.chips{display:flex;gap:4px;flex-wrap:wrap}
.chip{border:1px solid var(--line);background:var(--bg);color:var(--muted);border-radius:999px;font-size:11px;padding:3px 9px;cursor:pointer}
.chip.active{background:var(--card);color:var(--text);border-color:var(--blue)}

/* ---------- records ---------- */
.recitem{display:flex;align-items:center;gap:var(--s2);padding:8px 10px;border-bottom:1px solid #1c2128;font-size:11px;color:var(--text-dim)}
.recitem:last-child{border-bottom:0}
.recitem .t{color:var(--muted);flex-shrink:0}
.recitem .p{flex:1;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.tag{padding:1px 6px;border-radius:4px;font-size:10px;flex-shrink:0}

/* ---------- logs ---------- */
.logbox{max-height:240px;overflow:auto;padding:12px;background:var(--log-bg);border-radius:var(--r-md);border:1px solid var(--line);font-size:12px;color:var(--muted);white-space:pre-wrap;font-family:ui-monospace,Menlo,Consolas,monospace}
.logline{display:block}
.logline .ts{color:var(--muted)}
.logline .tx{color:var(--text-dim)}
.logline.err .tx{color:var(--danger-text)}
.logline.warn .tx{color:var(--warning-text)}
.logline.del .tx{color:var(--danger-text);font-weight:700}
.backlatest{position:sticky;bottom:8px;display:inline-block;background:var(--info-bg);color:#fff;border:0;border-radius:999px;padding:6px 12px;font-size:12px;cursor:pointer}

/* ---------- banners / empty / skeleton ---------- */
.banner{padding:10px 12px;border-radius:var(--r-md);font-size:12px;margin-bottom:var(--s3);border:1px solid}
.banner-warn{background:var(--warning-bg);border-color:var(--warning);color:var(--warning-text)}
.banner-danger{background:var(--bad-bg);border-color:var(--danger);color:var(--danger-text)}
.banner-info{background:rgba(88,166,255,.08);border-color:var(--info);color:var(--blue)}
.empty{text-align:center;padding:var(--s5) 12px;color:var(--muted)}
.empty .ico{font-size:32px;display:block;margin-bottom:var(--s2)}
.empty .t{color:var(--text-dim);font-size:14px;margin-bottom:4px}
.empty .t.ok{color:var(--ok-text)}
.skel{display:inline-block;min-width:44px;height:12px;border-radius:4px;background:linear-gradient(90deg,var(--card),#2b313a,var(--card));background-size:200% 100%;animation:sk 1.2s linear infinite;vertical-align:middle}
@keyframes sk{0%{background-position:200% 0}100%{background-position:-200% 0}}

/* ---------- toast / modal / busy ---------- */
#toastRoot{position:fixed;right:18px;bottom:18px;z-index:var(--z-toast);display:flex;flex-direction:column;gap:8px;align-items:flex-end}
.toast{border-radius:var(--r-lg);padding:11px 14px;font-size:13px;box-shadow:0 8px 24px rgba(0,0,0,.4);max-width:340px;border:1px solid;cursor:pointer}
.toast.success{background:var(--ok-bg);color:var(--ok-text);border-color:#26a641}
.toast.error{background:var(--bad-bg);color:var(--bad-text);border-color:var(--danger-bg)}
.toast.info{background:var(--card);color:var(--text);border-color:var(--line)}
.toast.warn{background:var(--warning-bg);color:var(--warning-text);border-color:var(--warning)}
#modalRoot{position:fixed;inset:0;z-index:var(--z-modal);display:none}
.modal-mask{position:absolute;inset:0;background:rgba(1,4,9,.72);display:flex;align-items:center;justify-content:center;padding:16px;animation:fade .12s ease}
@keyframes fade{from{opacity:0}to{opacity:1}}
.modal{width:440px;max-width:100%;background:var(--panel);border:1px solid var(--line);border-radius:var(--r-xl);box-shadow:0 16px 48px rgba(0,0,0,.55);padding:18px;animation:rise .12s ease}
@keyframes rise{from{transform:translateY(8px);opacity:0}to{transform:none;opacity:1}}
.modal-title{font-size:15px;font-weight:700;color:var(--text);margin-bottom:10px}
.modal-body{font-size:13px;color:var(--text-dim);line-height:1.7}
.modal-list{margin:6px 0;padding-left:18px}
.modal-list li{margin:2px 0}
.modal-input{width:100%;margin-top:10px;background:var(--bg);border:1px solid var(--line);color:var(--text);border-radius:var(--r-md);padding:9px 11px;font-size:13px;height:36px}
.modal-actions{display:flex;justify-content:flex-end;gap:var(--s2);margin-top:var(--s4)}
.modal.wide{width:640px}
.dirpick{max-height:320px;overflow:auto;border:1px solid var(--line);border-radius:var(--r-md);background:var(--bg)}
.dirpick .diritem:last-child{border-bottom:0}
#busy{position:fixed;inset:0;z-index:var(--z-busy);display:none;align-items:center;justify-content:center;background:rgba(1,4,9,.35)}
.busy-inner{background:var(--panel);border:1px solid var(--line);border-radius:var(--r-lg);padding:16px 20px;display:flex;align-items:center;gap:10px;box-shadow:0 8px 24px rgba(0,0,0,.4);font-size:13px}

/* ---------- responsive ---------- */
@media(max-width:1199px){.grid,.grid2{grid-template-columns:1fr}}
@media(max-width:767px){
 .main{padding:12px}.topbar{padding:8px 12px}
 .row2,.row3{grid-template-columns:1fr}
 .statgrid{grid-template-columns:1fr 1fr}
 .btn{min-height:44px}
 .tab{padding:12px 10px}
 .mini,.chip,table.tbl th.sortable{min-height:44px}
 .mini,.chip{display:inline-flex;align-items:center}
 .tb-meta.suggest{display:none}
 #toastRoot{left:12px;right:12px;bottom:12px;align-items:stretch}
 .modal-mask{align-items:flex-end;padding:0}
 .modal{width:100%;border-radius:16px 16px 0 0}
}
</style>
</head>
<body>
<div class="head">
  <div class="topbar">
    <div class="tb-row">
      <div class="brand">
        <svg viewBox="0 0 24 24" fill="none" stroke="#58a6ff" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 6h18"/><path d="M8 6V4h8v2"/><path d="M6 6l1 14h10l1-14"/></svg>
        <h1>NetDrive Sweeper</h1>
      </div>
      <div class="conn"><span class="dot" id="connDot"></span><span id="connText"><span class="skel"></span></span></div>
      <div class="perms" id="permBadges"><span class="badge neutral">权限未读取</span></div>
      <div class="tb-spacer"></div>
      <span class="dirty hidden" id="dirtyFlag">● 有未保存的修改</span>
      <button class="btn btn-primary" id="saveBtn">保存配置</button>
    </div>
    <div class="tb-row">
      <span class="tb-meta">Token 根目录: <span id="tokenRoot">-</span></span>
      <span class="tb-meta" id="runState">空闲</span>
      <div class="tb-spacer"></div>
      <span class="tb-meta suggest" id="nextAction"></span>
    </div>
    <div class="progress" id="progress"></div>
  </div>
  <nav class="tabs" id="tabs">
    <button class="tab" data-tab="conn">① 连接与目录<span class="badge count hidden" id="tabConnCount"></span></button>
    <button class="tab" data-tab="rules">② 清理规则<span class="dotmini hidden" id="tabRulesDot"></span></button>
    <button class="tab active" data-tab="run">③ 执行与结果</button>
    <button class="tab" data-tab="logs">④ 记录与日志</button>
  </nav>
</div>

<main class="main">
  <!-- ① 连接与目录 -->
  <section class="tabpane" id="tab-conn">
    <div class="grid2">
      <div class="col">
        <div class="card">
          <h2>① 连接与目录 · CD2 连接</h2>
          <div class="row2">
            <div class="formgroup"><label>gRPC 地址</label><input id="address" placeholder="127.0.0.1:19798"><div class="help">CD2 的 gRPC 端口，默认 127.0.0.1:19798</div></div>
            <div class="formgroup"><label>API Token</label>
              <div class="inline"><input id="token" type="password" autocomplete="off"><button type="button" class="btn btn-ghost btn-sm" id="toggleToken">显示</button></div>
              <div class="help">在 CD2 中创建，需含 allow_list / allow_delete（清理时）/ allow_push_message（事件驱动时）</div>
            </div>
          </div>
          <div class="actions">
            <button class="btn btn-ok" id="testBtn">测试连接</button>
            <button class="btn btn-primary" id="saveBtn2">保存配置</button>
          </div>
          <div class="help" style="margin-top:8px">测试连接会先保存当前配置（如有未保存修改会先询问）。</div>
        </div>
      </div>
      <div class="col">
        <div class="card">
          <h2>清理目录<span class="spacer"></span><button class="btn btn-ok btn-sm" id="openDirPick">＋ 添加目录</button><span class="badge count" id="taskCount">0</span></h2>
          <div class="sub">点击「＋ 添加目录」从网盘弹框浏览选择；列表为空时不会扫描任何目录（不会隐式扫全盘）。</div>
          <div class="panel" id="taskList"></div>
          <div class="actions"><button class="btn btn-danger" id="clearTasksBtn">清空目录</button></div>
        </div>
      </div>
    </div>
  </section>

  <!-- ② 清理规则 -->
  <section class="tabpane" id="tab-rules">
    <div class="card">
      <h2>② 清理规则</h2>
      <div class="row2">
        <div class="formgroup"><label>广告后缀</label><input id="adExts" value=".txt,.html,.url,.lnk"><div class="help">命中即判为垃圾（逗号分隔）</div></div>
        <div class="formgroup"><label>视频后缀</label><input id="videoExts" value=".mp4,.mkv,.ts"><div class="help">配合下方阈值按体积判定</div></div>
        <div class="formgroup"><label>小视频阈值 MB</label><input id="sizeLimit" type="number" step="0.1" value="20"><div class="help">视频后缀且体积 ≤ 此值即命中。默认 20 MB</div></div>
        <div class="formgroup"><label>限速 ops/秒</label><input id="opsPerSec" type="number" step="0.1" value="5"><div class="help warn">每次 gRPC 调用前取令牌。默认 5，对齐 115 官方上限，调高会增加风控风险</div></div>
        <div class="formgroup"><label>文件冷却小时</label><input id="cooldown" type="number" value="0"><div class="help">设为 0 = 立即清理（发现即删）；设置 >0 则新文件在该冷却期内跳过。默认 0</div></div>
        <div class="formgroup"><label>排除关键词</label><input id="excludeDirs" value="重要,备份"><div class="help">目录名包含任一关键词即整目录跳过。重要目录务必填入</div></div>
        <div class="formgroup"><label>推送防抖秒数</label><input id="pushDebounce" type="number" value="5"><div class="help">事件驱动下合并突发变更的静默窗口。默认 5 秒（修改后需重启生效）</div></div>
        <div class="formgroup"><label>未完成后缀</label><input id="incompleteSuffixes" value=".part,.download,.!qB,.bc!,.aria2,.crdownload,.td,.tmp,.!ut"><div class="help">含这些后缀的目录整目录跳过。留空会自动回填默认值，不建议清空</div></div>
      </div>
      <div class="checks">
        <label class="check warn"><input id="forceRefresh" type="checkbox">强制刷新 CD2 缓存（会显著增加网盘 API 压力，非必要不建议开启）</label>
        <label class="check"><input id="offlineOnly" type="checkbox" checked>只清理已完成离线任务</label>
        <label class="check"><input id="deletePermanently" type="checkbox">永久删除（不进回收站）</label>
        <label class="check warn"><input id="allowDelete" type="checkbox">允许自动清理（删除总开关）</label>
        <label class="check"><input id="enablePush" type="checkbox" checked>启用事件驱动实时清理（PushMessage，修改后需重启容器生效）</label>
        <div class="help warn hidden" id="pushWarn">当前 Token 缺少 allow_push_message，事件驱动实时清理不会生效。请在 CD2 中为该 Token 勾选该权限。</div>
      </div>
      <details class="adv" id="advBox">
        <summary>高级（保险丝与限速）</summary>
        <div class="adv-body">
          <div class="row2">
            <div class="formgroup"><label>单轮文件数上限</label><input id="maxFilesPerRun" type="number" value="2000"><div class="help">单轮删除文件数上限（保险丝）。默认 2000</div></div>
            <div class="formgroup"><label>单轮总字节上限 GiB</label><input id="maxTotalBytes" type="number" step="0.5" value="10"><div class="help">单轮删除总字节上限。默认 10 GiB</div></div>
            <div class="formgroup"><label>令牌桶突发容量</label><input id="burst" type="number" value="10"><div class="help">令牌桶突发容量。默认 10</div></div>
            <div class="formgroup"><label>最大递归深度</label><input id="maxDepth" type="number" value="0"><div class="help warn">0 = 不限递归深度（默认）。过大值会自动收敛为 100</div></div>
          </div>
          <div class="help">这 4 项是产品宣称的「多重保险丝」，此前未在 UI 暴露且保存时会被静默重置，现已纳入并修复。</div>
        </div>
      </details>
    </div>
  </section>

  <!-- ③ 执行与结果 -->
  <section class="tabpane active" id="tab-run">
    <div class="card" id="guideCard">
      <div class="guide-head"><h2 style="margin:0">首次配置引导</h2><span class="spacer"></span><span class="guide-toggle" id="guideToggle">收起</span></div>
      <div class="stepper" id="stepper"></div>
      <div class="sub" id="guideHint"></div>
    </div>
    <div class="card">
      <h2>③ 执行与结果</h2>
      <div class="banner banner-warn hidden" id="noTaskBanner">尚未配置任何清理目录，无法扫描。请到「① 连接与目录」添加目录。</div>
      <div class="statgrid">
        <div class="stat"><span class="label">检查文件</span><span class="value" id="statChecked"><span class="skel"></span></span></div>
        <div class="stat"><span class="label">命中垃圾</span><span class="value" id="statMatched"><span class="skel"></span></span></div>
        <div class="stat"><span class="label">已删除</span><span class="value" id="statDeleted"><span class="skel"></span></span></div>
        <div class="stat"><span class="label">跳过/错误</span><span class="value" id="statSkipped"><span class="skel"></span></span></div>
      </div>
      <div class="actions">
        <button class="btn btn-primary" id="scanBtn">扫描预览</button>
        <button class="btn btn-danger" id="cleanBtn">执行清理</button>
        <button class="btn btn-ghost" id="recRefreshRun" style="margin-left:auto">刷新记录</button>
      </div>
    </div>
    <div class="card">
      <h2>扫描结果<span class="spacer"></span>
        <button class="btn btn-ghost btn-sm" id="copyListBtn">复制命中清单</button>
        <button class="btn btn-ghost btn-sm" id="markAllBtn">全部标记已复核</button>
        <button class="btn btn-ghost btn-sm" id="unmarkAllBtn">取消标记</button>
      </h2>
      <div class="banner banner-info">清理为整批执行，不支持按条挑选；下方「●」仅表示「这条我已人工看过」，不影响执行范围。</div>
      <div class="tblbar">
        <input type="text" id="tblSearch" placeholder="按路径关键字过滤…">
        <div class="chips" id="reasonChips">
          <span class="chip active" data-reason="all">全部原因</span>
          <span class="chip" data-reason="suffix">后缀</span>
          <span class="chip" data-reason="video">小体积</span>
        </div>
        <select id="tblExt"><option value="">全部后缀</option></select>
        <label class="check"><input type="checkbox" id="tblBigOnly">仅 &gt; 1MB</label>
        <span class="muted" id="tblSummary"></span>
      </div>
      <div class="tablewrap">
        <table class="tbl" id="resultTbl">
          <thead><tr>
            <th class="mark" title="标记已复核">●</th>
            <th class="sortable" data-sort="path">文件路径 <span id="dir-path"></span></th>
            <th class="sortable r" data-sort="size">大小 <span id="dir-size"></span></th>
            <th>命中原因</th>
          </tr></thead>
          <tbody id="resultBody"><tr><td colspan="4"><div class="empty"><span class="ico">🔍</span><span class="t">还没有扫描结果</span><div>点上方「扫描预览」查看会命中哪些文件</div></div></td></tr></tbody>
        </table>
      </div>
      <div class="pager" id="tblPager"></div>
      <div id="offlineBox" style="margin-top:12px"></div>
      <div id="errBox" style="margin-top:8px"></div>
    </div>
  </section>

  <!-- ④ 记录与日志 -->
  <section class="tabpane" id="tab-logs">
    <div class="grid2">
      <div class="col">
        <div class="card">
          <h2>清理记录<span class="spacer"></span><button class="btn btn-ghost btn-sm" id="recordsBtn">刷新</button><span class="muted" id="recCount"></span></h2>
          <div class="tblbar">
            <input type="text" id="recSearch" placeholder="按路径/结果过滤…">
            <div class="chips" id="recChips">
              <span class="chip active" data-rec="all">全部</span>
              <span class="chip" data-rec="ok">成功</span>
              <span class="chip" data-rec="fail">失败</span>
            </div>
          </div>
          <div class="panel" id="recPanel"><div class="list-empty"><span class="ico">📄</span><span class="t">暂无清理记录</span><div>执行清理或启用事件驱动后，记录会显示在这里</div></div></div>
        </div>
      </div>
      <div class="col">
        <div class="card">
          <h2>运行日志<span class="spacer"></span><button class="btn btn-ghost btn-sm" id="logCopyBtn">复制</button><button class="btn btn-ghost btn-sm" id="logsBtn">刷新</button><button class="btn btn-danger btn-sm" id="clearLogsBtn">清空</button></h2>
          <div class="tblbar">
            <input type="text" id="logSearch" placeholder="按关键字过滤日志…">
            <div class="chips" id="logChips">
              <span class="chip active" data-log="all">全部</span>
              <span class="chip" data-log="error">错误</span>
              <span class="chip" data-log="warn">警告</span>
            </div>
            <label class="check"><input type="checkbox" id="logFollow" checked>跟随最新</label>
            <button class="backlatest hidden" id="logBackBottom">↓ 回到最新</button>
          </div>
          <div class="logbox" id="logsBox"><div class="list-empty"><span class="ico">📝</span><span class="t">暂无运行日志</span></div></div>
        </div>
      </div>
    </div>
  </section>
</main>

<div id="modalRoot"></div>
<div id="toastRoot"></div>
<div id="busy"><div class="busy-inner"><span class="spinner dark"></span><span id="busyText">运行中…</span></div></div>
<input id="tasksHidden" type="hidden">
<script>
var state={},dirty=false,lastScan=null,lastToken=null,scanRun=false,savedOnce=false;
var progress={conn:false,dir:false,rules:false,preview:false,enable:false};
var tbl={page:1,pageSize:50,search:'',reason:'all',ext:'',bigOnly:false,sortKey:'path',sortDir:'asc',reviewed:{}};
var logState={level:'all',search:'',follow:true,raw:''};
var recState={result:'all',search:''};
try{var gp=JSON.parse(localStorage.getItem('nds_progress')||'{}');for(var k in gp)progress[k]=gp[k];}catch(e){}

function el(id){return document.getElementById(id)}
function val(id){var e=el(id);return e?e.value:''}
function setv(id,v){el(id).value=(v===undefined||v===null)?'':v}
function checked(id){var e=el(id);return e?e.checked:false}
function esc(s){return String(s===undefined||s===null?'':s).replace(/[&<>"']/g,function(m){return {'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[m]})}
function saveProgress(){try{localStorage.setItem('nds_progress',JSON.stringify(progress))}catch(e){}}

function api(url,opt){return fetch(url,Object.assign({cache:'no-store',headers:{'Content-Type':'application/json'}},opt||{})).then(function(r){return r.json().then(function(j){if(!r.ok||j.ok===false)throw new Error(j.error||'请求失败');return j})})}

/* ---------- toast ---------- */
function toast(msg,type){
  var root=el('toastRoot');var d=document.createElement('div');
  type=type||'info';d.className='toast '+type;d.textContent=msg;
  d.addEventListener('click',function(){d.remove()});
  root.appendChild(d);
  while(root.children.length>3)root.removeChild(root.firstChild);
  var ms=type==='error'?6000:2800;
  setTimeout(function(){d.remove()},ms);
}
function copyText(txt){
  function fb(){var ta=document.createElement('textarea');ta.value=txt;ta.style.position='fixed';ta.style.left='-9999px';document.body.appendChild(ta);ta.focus();ta.select();try{document.execCommand('copy');toast('已复制','success')}catch(e){toast('复制失败，请手动选择','error')}document.body.removeChild(ta)}
  if(navigator.clipboard&&window.isSecureContext){navigator.clipboard.writeText(txt).then(function(){toast('已复制','success')},fb)}else{fb()}
}

/* ---------- dirty ---------- */
function setDirty(v){dirty=v;el('dirtyFlag').classList.toggle('hidden',!v);el('tabRulesDot').classList.toggle('hidden',!v);el('saveBtn').classList.toggle('btn-primary',v);}

/* ---------- busy / run feedback ---------- */
var runTimer=null,runStart=0;
function tickRun(){var s=Math.floor((Date.now()-runStart)/1000);var t='运行中 · 已耗时 '+s+'s';if(s>30)t+=' · 仍在进行，请勿关闭页面';el('runState').textContent=t;el('busyText').textContent=t;}
function startRun(btn,label){
  if(btn){btn.dataset.orig=btn.textContent;btn.disabled=true;btn.innerHTML='<span class="spinner"></span>'+esc(label)}
  el('busy').style.display='flex';el('progress').classList.add('on');
  runStart=Date.now();tickRun();runTimer=setInterval(tickRun,1000);
  el('scanBtn').disabled=true;el('cleanBtn').disabled=true;
}
function endRun(){
  if(runTimer){clearInterval(runTimer);runTimer=null}
  el('busy').style.display='none';el('progress').classList.remove('on');el('runState').textContent='空闲';
  ['scanBtn','cleanBtn'].forEach(function(id){var b=el(id);b.disabled=false});
  ['scanBtn','cleanBtn'].forEach(function(id){var b=el(id);if(b.dataset.orig){b.textContent=b.dataset.orig;delete b.dataset.orig}});
}

/* ---------- modal ---------- */
function confirmDialog(opts){
  return new Promise(function(resolve){
    var root=el('modalRoot');root.style.display='block';root.innerHTML='';
    var mask=document.createElement('div');mask.className='modal-mask';
    var box=document.createElement('div');box.className='modal';box.setAttribute('role','dialog');box.setAttribute('aria-modal','true');
    var html='<div class="modal-title">'+esc(opts.title||'确认')+'</div>';
    html+='<div class="modal-body">'+(opts.bodyHtml||esc(opts.body||''))+'</div>';
    if(opts.confirmWord){
      html+='<input id="modalWord" class="modal-input" autocomplete="off" placeholder="'+esc('请输入 '+opts.confirmWord+' 以确认')+'">';
      html+='<div class="actions" style="margin-top:6px"><button type="button" class="btn btn-ghost btn-sm" id="modalCopyWord">复制 '+esc(opts.confirmWord)+'</button></div>';
    }
    html+='<div class="modal-actions"><button type="button" class="btn btn-ghost" id="modalCancel">取消</button><button type="button" class="btn btn-danger" id="modalOk" '+(opts.confirmWord?'disabled':'')+'>'+esc(opts.okText||'确认')+'</button></div>';
    box.innerHTML=html;mask.appendChild(box);root.appendChild(mask);
    var okBtn=el('modalOk');
    function close(v){root.style.display='none';root.innerHTML='';document.removeEventListener('keydown',onKey);resolve(v)}
    function onKey(e){if(e.key==='Escape')close(false)}
    mask.addEventListener('click',function(e){if(e.target===mask)close(false)});
    el('modalCancel').addEventListener('click',function(){close(false)});
    if(opts.confirmWord){
      var inp=el('modalWord');
      inp.addEventListener('input',function(){okBtn.disabled=inp.value!==opts.confirmWord});
      el('modalCopyWord').addEventListener('click',function(){copyText(opts.confirmWord)});
      setTimeout(function(){inp.focus()},30);
    }else{setTimeout(function(){okBtn.focus()},30)}
    okBtn.addEventListener('click',function(){if(!okBtn.disabled)close(true)});
    document.addEventListener('keydown',onKey);
  });
}

/* ---------- status ---------- */
function setConn(state,info,msg){
  var dot=el('connDot'),txt=el('connText');
  dot.className='dot';
  if(state==='ok'){dot.classList.add('ok');txt.textContent='已连接'+(info&&info.rootDir?' '+info.rootDir:'')}
  else if(state==='connecting'){dot.classList.add('connecting');txt.textContent='连接中…'}
  else if(state==='fail'){dot.classList.add('fail');txt.textContent='连接失败'+(msg?': '+msg:'')}
  else{txt.textContent='未连接'}
}
var PERM=[['list','allowList','allow_list'],['delete','allowDelete','allow_delete'],['perm_delete','allowDeletePermanently','allow_delete_permanently'],['push_message','allowPushMessage','allow_push_message']];
function renderPerms(info){
  var box=el('permBadges');
  if(!info){box.innerHTML='<span class="badge neutral">权限未读取</span>';renderPushWarn();return}
  box.innerHTML=PERM.map(function(p){
    var has=!!info[p[1]];
    var tip=has?'':('缺少 '+p[2]+' → '+({'list':'无法读取目录','delete':'无法删除到回收站','perm_delete':'无法永久删除','push_message':'事件驱动实时清理不会生效'}[p[0]]));
    return '<span class="badge '+(has?'ok':'bad')+'" title="'+esc(tip)+'">'+p[0]+'</span>';
  }).join('');
  renderPushWarn();
}
// 常驻可见的推送权限告警：勾了「事件驱动实时清理」但 Token 无 allow_push_message 时提示。
// 修复 §0.3 #7 痛点——此前只在徽章 title 挂悬浮提示，移动端/不悬浮完全看不到，导致静默失效无解释。
function renderPushWarn(){
  var show=checked('enablePush')&&lastToken&&!lastToken.allowPushMessage;
  el('pushWarn').classList.toggle('hidden',!show);
}
function updateNextAction(){
  var a='';
  if(!progress.conn)a='建议：先到「① 连接与目录」测试连接';
  else if(taskList().length<1)a='建议：添加至少 1 个清理目录';
  else if(!lastScan)a='建议：用「扫描预览」确认命中结果后再启用自动清理';
  else if(!checked('allowDelete'))a='建议：确认无误后勾选「允许自动清理」以启用删除';
  else a='一切就绪，正在按规则运行';
  el('nextAction').textContent=a;
}

/* ---------- tabs ---------- */
function switchTab(name){
  document.querySelectorAll('.tab').forEach(function(t){t.classList.toggle('active',t.dataset.tab===name)});
  document.querySelectorAll('section.tabpane').forEach(function(s){s.classList.toggle('active',s.id==='tab-'+name)});
  window.scrollTo({top:0,behavior:'smooth'});
}
el('tabs').addEventListener('click',function(e){var b=e.target.closest('.tab');if(b)switchTab(b.dataset.tab)});

/* ---------- guide ---------- */
function applyGuideCollapsed(v){
  var b=el('guideCard');b.dataset.collapsed=v?'1':'0';
  el('stepper').classList.toggle('hidden',!!v);
  el('guideHint').classList.toggle('hidden',!!v);
  el('guideToggle').textContent=v?'展开':'收起';
}
function renderGuide(){
  var steps=[['连接','测试连接'],['目录','添加目录'],['规则','确认规则'],['预览','扫描预览'],['启用','允许自动清理']];
  var done=[progress.conn,taskList().length>0,progress.rules,progress.preview,progress.enable];
  var cur=done.indexOf(false);if(cur<0)cur=steps.length;
  var h='';
  for(var i=0;i<steps.length;i++){
    if(i>0)h+='<span class="stepline"></span>';
    var cls=i<cur?'done':(i===cur?'cur':'');
    var num=i<cur?'✓':String(i+1);
    h+='<div class="stepdot '+cls+'"><div class="num">'+num+'</div><div class="lbl">'+esc(steps[i][0])+'</div></div>';
  }
  el('stepper').innerHTML=h;
  var doneAll=cur>=steps.length;
  el('guideHint').textContent=doneAll?'首次配置已完成 ✓ 如需重新查看，点「收起/展开」切换。':('下一步：'+steps[cur][1]+'。');
  if(doneAll){
    var gs;try{gs=localStorage.getItem('nds_guideCollapsed')}catch(e){gs=null}
    applyGuideCollapsed(gs===null?true:(gs==='1'));
  }
}

/* ---------- tasks ---------- */
function taskList(){var v=val('tasksHidden');if(!v)return[];return v.split('\n').map(function(x){return x.trim()}).filter(Boolean)}
function setTaskList(list){setv('tasksHidden',list.join('\n'));renderTaskList(list);renderGuide();updateNextAction();guardEmptyTasks()}
function renderTaskList(list){
  el('taskCount').textContent=list.length;
  el('tabConnCount').textContent=list.length;
  el('tabConnCount').classList.toggle('hidden',list.length===0);
  if(!list.length){el('taskList').innerHTML='<div class="list-empty"><span class="ico">📁</span><span class="t">尚未添加清理目录</span><div>从右侧「目录浏览」选择目录后点「加入」</div></div>';return}
  el('taskList').innerHTML=list.map(function(p,i){
    return '<div class="taskitem"><span class="path mono" title="'+esc(p)+'">'+esc(p)+'</span>'
      +'<button class="mini" data-act="up" data-i="'+i+'" '+(i===0?'disabled':'')+'>↑</button>'
      +'<button class="mini" data-act="down" data-i="'+i+'" '+(i===list.length-1?'disabled':'')+'>↓</button>'
      +'<button class="mini del" data-act="del" data-i="'+i+'">× 移除</button></div>';
  }).join('');
}
function taskListAction(act,i){
  var t=taskList();
  if(act==='del')t.splice(i,1);
  else if(act==='up'&&i>0){var x=t[i];t[i]=t[i-1];t[i-1]=x}
  else if(act==='down'&&i<t.length-1){var y=t[i];t[i]=t[i+1];t[i+1]=y}
  setTaskList(t);setDirty(true);
}
el('taskList').addEventListener('click',function(e){var b=e.target.closest('.mini');if(b)taskListAction(b.dataset.act,parseInt(b.dataset.i,10))});
function addTask(p){var t=taskList();if(t.indexOf(p)>=0){toast('该目录已在列表中，未重复添加','warn');return}t.push(p);setTaskList(t);setDirty(true);toast('已添加目录 '+p,'success')}
function guardEmptyTasks(){var empty=taskList().length===0;el('noTaskBanner').classList.toggle('hidden',!empty);el('scanBtn').disabled=empty;el('cleanBtn').disabled=empty;}

/* ---------- directory picker (modal) ---------- */
var dirPick={path:'/',open:false};
function openDirPicker(){
  dirPick.path='/';
  var root=el('modalRoot');root.style.display='block';root.innerHTML='';
  var mask=document.createElement('div');mask.className='modal-mask';
  var box=document.createElement('div');box.className='modal wide';box.setAttribute('role','dialog');box.setAttribute('aria-modal','true');
  box.innerHTML='<div class="modal-title">浏览目录并添加</div>'
    +'<div class="crumbs" id="dpCrumbs"></div>'
    +'<div class="dirpick" id="dpList"><div class="list-empty">加载中…</div></div>'
    +'<div class="modal-actions"><button type="button" class="btn btn-ok" id="dpAddCur">＋ 添加当前路径</button><button type="button" class="btn btn-primary" id="dpAddSel">＋ 添加选中 (0)</button><button type="button" class="btn btn-ghost" id="dpClose">关闭</button></div>';
  mask.appendChild(box);root.appendChild(mask);
  function close(){dirPick.open=false;root.style.display='none';root.innerHTML='';document.removeEventListener('keydown',onKey)}
  function onKey(e){if(e.key==='Escape')close()}
  mask.addEventListener('click',function(e){if(e.target===mask)close()});
  el('dpClose').addEventListener('click',close);
  document.addEventListener('keydown',onKey);
  el('dpCrumbs').addEventListener('click',function(e){var c=e.target.closest('.c');if(c)dpList(c.dataset.p)});
  el('dpList').addEventListener('click',function(e){
    var en=e.target.closest('[data-enter]');if(en){dpList(en.dataset.enter);return}
    var ad=e.target.closest('[data-add]');if(ad){addTask(ad.dataset.add);dpList();return}
  });
  el('dpList').addEventListener('change',function(e){if(e.target.classList&&e.target.classList.contains('dirchk'))dpUpdateSel()});
  el('dpAddCur').addEventListener('click',function(){addTask(dirPick.path);dpList()});
  el('dpAddSel').addEventListener('click',function(){var sel=document.querySelectorAll('#dpList .dirchk:checked');if(!sel.length){toast('请先勾选要加入的子目录','warn');return}sel.forEach(function(c){addTask(c.dataset.path)});dpList()});
  dirPick.open=true;
  dpList('/');
}
function dpCrumbs(path){
  path=path||'/';
  var parts=path.split('/').filter(Boolean);
  var h='<span class="c" data-p="/">根目录</span>';
  var acc='';
  for(var i=0;i<parts.length;i++){acc+='/'+parts[i];h+='<span class="sep">›</span>';
    if(i===parts.length-1)h+='<span class="cur">'+esc(parts[i])+'</span>';
    else h+='<span class="c" data-p="'+esc(acc)+'">'+esc(parts[i])+'</span>';}
  el('dpCrumbs').innerHTML=h;
}
function dpUpdateSel(){var n=document.querySelectorAll('#dpList .dirchk:checked').length;el('dpAddSel').textContent='＋ 添加选中 ('+n+')';el('dpAddSel').disabled=n===0}
function dpList(p){
  var path=p||dirPick.path||'/';
  dirPick.path=path;dpCrumbs(path);
  el('dpList').innerHTML='<div class="list-empty">加载中…</div>';
  return api('/api/list?path='+encodeURIComponent(path)+'&_='+Date.now()).then(function(j){
    if(j.token){lastToken=j.token;renderPerms(j.token);setConn('ok',j.token)}
    var rows=[];
    var cur=taskList();
    for(var i=0;i<j.dirs.length;i++){var d=j.dirs[i];var added=cur.indexOf(d.path)>=0;
      rows.push('<div class="diritem"><input type="checkbox" class="dirchk" data-path="'+esc(d.path)+'" '+(added?'disabled':'')+'>'
        +'<span class="name" data-enter="'+esc(d.path)+'">'+esc(d.name)+'</span>'
        +(added?'<span class="added">✓ 已添加</span>':'<button class="mini" data-add="'+esc(d.path)+'">＋ 加入</button>')+'</div>');}
    el('dpList').innerHTML=rows.length?rows.join(''):'<div class="list-empty"><span class="ico">📂</span><span class="t">此目录下没有子文件夹</span><div>可点上方「＋ 添加当前路径」直接把当前目录加入</div></div>';
    dpUpdateSel();
  }).catch(function(e){toast(e.message,'error');el('dpList').innerHTML='<div class="list-empty"><span class="t danger-text">'+esc(e.message)+'</span></div>'});
}

/* ---------- config in/out ---------- */
function fillConfig(c){
  setv('address',c.address);setv('token',c.token);
  setv('adExts',c.ad_exts);setv('videoExts',c.video_exts);setv('sizeLimit',c.size_limit_mb);
  setv('opsPerSec',c.ops_per_sec);setv('cooldown',c.file_cooldown_hours);setv('excludeDirs',c.exclude_dirs);
  setv('pushDebounce',c.push_debounce_seconds);setv('incompleteSuffixes',c.incomplete_suffixes);
  setv('maxFilesPerRun',c.max_files_per_run);setv('maxTotalBytes',Math.round((c.max_total_bytes||0)/1073741824*100)/100);setv('burst',c.burst);setv('maxDepth',c.max_depth);
  el('forceRefresh').checked=!!c.force_refresh;
  el('offlineOnly').checked=c.offline_only!==false;
  el('deletePermanently').checked=!!c.delete_permanently;
  el('allowDelete').checked=!!c.allow_delete;
  el('enablePush').checked=c.enable_push!==false;
  setTaskList(c.tasks&&c.tasks.length?c.tasks:[]);
  setDirty(false);renderGuide();updateNextAction();renderPushWarn();
}
function gatherCfg(){
  return {
    address:val('address'),token:val('token'),ad_exts:val('adExts'),video_exts:val('videoExts'),
    size_limit_mb:parseFloat(val('sizeLimit')||0),exclude_dirs:val('excludeDirs'),
    ops_per_sec:parseFloat(val('opsPerSec')||5),file_cooldown_hours:parseInt(val('cooldown')||0),
    incomplete_suffixes:val('incompleteSuffixes'),enable_push:checked('enablePush'),
    push_debounce_seconds:parseInt(val('pushDebounce')||5),force_refresh:checked('forceRefresh'),
    offline_only:checked('offlineOnly'),delete_permanently:checked('deletePermanently'),allow_delete:checked('allowDelete'),
    max_files_per_run:parseInt(val('maxFilesPerRun')||2000),
    max_total_bytes:Math.round(parseFloat(val('maxTotalBytes')||10)*1073741824),
    burst:parseInt(val('burst')||10),max_depth:parseInt(val('maxDepth')||0),
    tasks:taskList()
  };
}
function saveCfg(btn){
  if(btn){btn.dataset.orig=btn.textContent;btn.disabled=true;btn.textContent='保存中…'}
  return api('/api/save',{method:'POST',body:JSON.stringify(gatherCfg())}).then(function(j){
    if(j.config)fillConfig(j.config);
    savedOnce=true;progress.rules=true;saveProgress();renderGuide();
    setDirty(false);toast('配置已保存','success');return j;
  }).finally(function(){if(btn){btn.disabled=false;if(btn.dataset.orig){btn.textContent=btn.dataset.orig;delete btn.dataset.orig}}});
}

/* ---------- test connection ---------- */
function testConn(){
  var go=function(){
    var btn=el('testBtn');
    btn.dataset.orig=btn.textContent;btn.disabled=true;btn.textContent='测试中…';
    setConn('connecting');
    var pre=dirty?saveCfg():Promise.resolve();
    pre.then(function(){return api('/api/test?_='+Date.now())}).then(function(j){
      lastToken=j.token;renderPerms(j.token);setConn('ok',j.token);
      progress.conn=true;saveProgress();renderGuide();updateNextAction();
      toast(j.message,'success');
    }).catch(function(e){setConn('fail',null,e.message);toast(e.message,'error')}).finally(function(){
      // §9.9 ③：测试期间按钮 disabled + 「测试中…」，成功/失败都要恢复。
      btn.disabled=false;if(btn.dataset.orig){btn.textContent=btn.dataset.orig;delete btn.dataset.orig}
    });
  };
  if(dirty){confirmDialog({title:'测试前需保存配置',body:'检测到未保存的修改，测试连接需要先保存当前配置，是否继续？',okText:'保存并测试'}).then(function(ok){if(ok)go()});}
  else go();
}

/* ---------- scan / clean ---------- */
function renderStats(j){
  el('statChecked').textContent=j.checked||0;el('statMatched').textContent=j.matched||0;
  el('statDeleted').textContent=j.deleted||0;
  el('statSkipped').textContent=(j.skipped||0)+((j.errors&&j.errors.length)?('+'+j.errors.length+'err'):'');
}
function reasonOf(item){
  var ext=('.'+(String(item.name).split('.').pop()||'')).toLowerCase();
  if(splitLower(val('adExts')).indexOf(ext)>=0)return 'suffix';
  return 'video';
}
function splitLower(s){return String(s||'').split(',').map(function(x){return x.trim().toLowerCase()}).filter(Boolean)}
function doScan(doDelete){
  if(taskList().length===0){toast('请先添加清理目录','error');switchTab('conn');return}
  if(doDelete&&!checked('allowDelete')){toast('请先勾选「允许自动清理」','error');switchTab('rules');return}
  var proceed=function(){
    var btn=doDelete?el('cleanBtn'):el('scanBtn');
    startRun(btn,doDelete?'清理中…':'扫描中…');
    api(doDelete?'/api/clean':'/api/scan').then(function(j){
      lastScan=j;renderStats(j);renderResult(j.items||[]);renderOffline(j.offlineTasks||[]);renderErrors(j.errors||[]);
      if(!doDelete){progress.preview=true;saveProgress()}
      if(doDelete)progress.enable=checked('allowDelete');saveProgress();renderGuide();updateNextAction();
      toast(doDelete?('清理完成：删除 '+j.deleted+' 个'):('扫描完成：命中 '+j.matched+' 个'),'success');
      if(doDelete)loadRecords().catch(function(){});
    }).catch(function(e){toast(e.message,'error');renderErrors([e.message])}).finally(function(){endRun()});
  };
  if(!doDelete){proceed();return}
  var it=(lastScan&&lastScan.items)||[];
  var sizeSum=it.reduce(function(a,x){return a+(x.size||0)},0);
  var permanent=checked('deletePermanently');
  var body='';
  if(!lastScan)body+='<div class="banner banner-warn">尚未「扫描预览」，确认后将按当前规则当场扫描并删除。</div>';
  body+='<ul class="modal-list">'
    +'<li>将删除 <b>'+(lastScan?(lastScan.matched||0):'（扫描后确定）')+'</b> 个文件，合计 <b>'+formatSize(sizeSum)+'</b></li>'
    +'<li>删除方式：'+(permanent?'<b class="danger-text">永久删除，不进回收站，不可恢复</b>':'进网盘回收站（可恢复）')+'</li>'
    +'<li>涉及 '+taskList().length+' 个目录</li>'
    +'<li>本轮跳过 '+(lastScan?(lastScan.skipped||0):0)+' 个（冷却期/未完成目录）</li></ul>';
  if(permanent)body+='<div class="banner banner-danger">永久删除不可恢复，请谨慎确认。</div>';
  confirmDialog({title:permanent?'确认永久删除？':'确认执行清理？',bodyHtml:body,okText:permanent?'永久删除':'确认清理',confirmWord:permanent?'DELETE':null}).then(function(ok){if(ok)proceed()});
}
function renderOffline(list){
  if(!list.length){el('offlineBox').innerHTML='';return}
  var h='<div class="sub" style="margin:0 0 6px">离线任务状态</div><table class="tbl"><thead><tr><th>目录</th><th>状态</th></tr></thead><tbody>';
  list.forEach(function(o){h+='<tr><td class="path mono" title="'+esc(o.path)+'">'+esc(o.path)+'</td><td>'+(o.ready?'<span style="color:var(--success-text)">'+esc(o.status)+'</span>':'<span style="color:var(--warning-text)">'+esc(o.status)+'</span>')+'</td></tr>'});
  el('offlineBox').innerHTML=h+'</tbody></table>';
}
function renderErrors(list){el('errBox').innerHTML=list.length?('<div class="banner banner-danger">'+list.map(esc).join('<br>')+'</div>'):''}

/* ---------- result table ---------- */
function currentRows(){
  var items=(lastScan&&lastScan.items)||[];
  items.forEach(function(x){x._reason=reasonOf(x)});
  var f=items.filter(function(x){
    if(tbl.search&&String(x.displayPath||x.path).toLowerCase().indexOf(tbl.search)<0)return false;
    if(tbl.reason!=='all'&&x._reason!==tbl.reason)return false;
    if(tbl.ext){var e='.'+String(x.name).split('.').pop().toLowerCase();if(e!==tbl.ext)return false}
    if(tbl.bigOnly&&!((x.size||0)>1048576))return false;
    return true;
  });
  f.sort(function(a,b){
    if(tbl.sortKey==='size'){return (a.size||0)-(b.size||0)}
    return String(a.displayPath||a.path).localeCompare(String(b.displayPath||b.path));
  });
  if(tbl.sortDir==='desc')f.reverse();
  return f;
}
function renderResult(items){
  el('tblExt').innerHTML='<option value="">全部后缀</option>'+uniqExts(items).map(function(e){return '<option value="'+esc(e)+'">'+esc(e)+'</option>'}).join('');
  renderResultTable();
}
function uniqExts(items){var s={};items.forEach(function(x){var e='.'+String(x.name).split('.').pop().toLowerCase();s[e]=1});return Object.keys(s).sort()}
function renderSortIndicator(){
  el('dir-path').textContent=tbl.sortKey==='path'?(tbl.sortDir==='asc'?'▲':'▼'):'';
  el('dir-size').textContent=tbl.sortKey==='size'?(tbl.sortDir==='asc'?'▲':'▼'):'';
}
function renderResultTable(){
  renderSortIndicator();
  var rows=currentRows();
  var total=rows.length,sizeSum=rows.reduce(function(a,x){return a+(x.size||0)},0);
  var reviewed=rows.filter(function(x){return tbl.reviewed[x.path]}).length;
  el('tblSummary').textContent='已复核 '+reviewed+' / 共 '+total+' · 合计 '+formatSize(sizeSum);
  var pages=Math.max(1,Math.ceil(total/tbl.pageSize));
  if(tbl.page>pages)tbl.page=pages;if(tbl.page<1)tbl.page=1;
  var start=(tbl.page-1)*tbl.pageSize;
  var page=rows.slice(start,start+tbl.pageSize);
  var body=el('resultBody');
  if(!total){
    var all=(lastScan&&lastScan.items)||[];
    body.innerHTML='<tr><td colspan="4"><div class="empty"><span class="ico">'+(all.length?'✅':'🔍')+'</span><span class="t'+(all.length?' ok':'')+'">'+(all.length?'未发现符合条件的垃圾文件':'还没有扫描结果')+'</span><div>'+(all.length?'当前规则下目录很干净；如需更严格，可调整②清理规则':'点上方「扫描预览」查看会命中哪些文件')+'</div></div></td></tr>';
    el('tblPager').innerHTML='';return;
  }
  body.innerHTML=page.map(function(x){
    var on=!!tbl.reviewed[x.path];
    return '<tr><td class="mark"><span class="markbtn '+(on?'on':'off')+'" data-mark="'+esc(x.path)+'" title="标记为已复核（不影响执行范围）">'+(on?'●':'○')+'</span></td>'
      +'<td class="path mono" title="'+esc(x.displayPath||x.path)+'">'+esc(x.displayPath||x.path)+'</td>'
      +'<td class="r">'+formatSize(x.size)+'</td>'
      +'<td>'+(x._reason==='suffix'?'后缀':'小体积')+'</td></tr>';
  }).join('');
  el('tblPager').innerHTML='<span>每页</span><select id="pageSize">'+[50,100,200].map(function(n){return '<option value="'+n+'" '+(n===tbl.pageSize?'selected':'')+'>'+n+'</option>'}).join('')+'</select>'
    +'<span>第 '+tbl.page+'/'+pages+' 页 · 共 '+total+' 条</span>'
    +'<button class="btn btn-ghost btn-sm" id="prevPage" '+(tbl.page<=1?'disabled':'')+'>上一页</button>'
    +'<button class="btn btn-ghost btn-sm" id="nextPage" '+(tbl.page>=pages?'disabled':'')+'>下一页</button>';
}
el('resultBody').addEventListener('click',function(e){var m=e.target.closest('[data-mark]');if(m){var p=m.dataset.mark;if(tbl.reviewed[p])delete tbl.reviewed[p];else tbl.reviewed[p]=1;renderResultTable()}});
el('tblPager').addEventListener('click',function(e){if(e.target.id==='prevPage'){tbl.page--;renderResultTable()}else if(e.target.id==='nextPage'){tbl.page++;renderResultTable()}});
el('tblPager').addEventListener('change',function(e){if(e.target.id==='pageSize'){tbl.pageSize=parseInt(e.target.value,10);tbl.page=1;renderResultTable()}});
el('tblSearch').addEventListener('input',function(){tbl.search=val('tblSearch').toLowerCase();tbl.page=1;renderResultTable()});
el('tblExt').addEventListener('change',function(){tbl.ext=val('tblExt');tbl.page=1;renderResultTable()});
el('tblBigOnly').addEventListener('change',function(){tbl.bigOnly=checked('tblBigOnly');tbl.page=1;renderResultTable()});
el('reasonChips').addEventListener('click',function(e){var c=e.target.closest('.chip');if(!c)return;tbl.reason=c.dataset.reason;el('reasonChips').querySelectorAll('.chip').forEach(function(x){x.classList.toggle('active',x===c)});tbl.page=1;renderResultTable()});
document.querySelectorAll('#resultTbl th.sortable').forEach(function(th){th.addEventListener('click',function(){var k=th.dataset.sort;if(tbl.sortKey===k)tbl.sortDir=(tbl.sortDir==='asc'?'desc':'asc');else{tbl.sortKey=k;tbl.sortDir='asc'}renderResultTable()})});
function copyList(){var rows=currentRows();if(!rows.length){toast('没有可复制的命中项','warn');return}copyText(rows.map(function(x){return (x.displayPath||x.path)+'  ('+formatSize(x.size)+')'}).join('\n'))}
function markAll(v){var rows=currentRows();rows.forEach(function(x){if(v)tbl.reviewed[x.path]=1;else delete tbl.reviewed[x.path]});renderResultTable()}

/* ---------- records ---------- */
function loadRecords(){
  return api('/api/records?_='+Date.now()).then(function(j){
    var list=(j.records||[]).map(function(r){try{return JSON.parse(r)}catch(e){return null}}).filter(Boolean).reverse();
    el('recCount').textContent='共 '+j.count+' 条';
    var q=recState.search;
    var f=list.filter(function(o){
      if(recState.result!=='all'&&o.result!==recState.result)return false;
      if(q&&String(o.displayPath||o.path||'').toLowerCase().indexOf(q)<0)return false;
      return true;
    }).slice(0,300);
    el('recPanel').innerHTML=f.length?f.map(function(o){
      var ok=o.result==='ok';
      return '<div class="recitem"><span class="t">'+esc(String(o.time||'').slice(0,19).replace('T',' '))+'</span>'
        +'<span class="tag" style="background:'+(ok?'var(--ok-bg)':'var(--bad-bg)')+';color:'+(ok?'var(--ok-text)':'var(--bad-text)')+'">'+esc(o.result)+'</span>'
        +'<span class="p" title="'+esc(o.displayPath||o.path)+'">'+esc(o.displayPath||o.path)+'</span>'
        +'<span class="tag" style="background:var(--bg);color:var(--muted)">'+esc(o.mode)+'</span></div>';
    }).join(''):'<div class="list-empty"><span class="ico">📄</span><span class="t">暂无清理记录</span><div>执行清理或启用事件驱动后，记录会显示在这里</div></div>';
  });
}
el('recSearch').addEventListener('input',function(){recState.search=val('recSearch').toLowerCase();loadRecords().catch(function(){})});
el('recChips').addEventListener('click',function(e){var c=e.target.closest('.chip');if(!c)return;recState.result=c.dataset.rec;el('recChips').querySelectorAll('.chip').forEach(function(x){x.classList.toggle('active',x===c)});loadRecords().catch(function(){})});

/* ---------- logs ---------- */
function classify(line){
  if(/ERROR|错误|失败|删除|DELETE/.test(line))return /删除|DELETE/.test(line)?'del':'err';
  if(/WARN|警告|跳过/.test(line))return 'warn';
  return '';
}
function syncLogBack(){el('logBackBottom').classList.toggle('hidden',!!logState.follow)}
function renderLogs(){
  var lines=(logState.raw||'').split('\n');
  if(lines.length>2000)lines=lines.slice(lines.length-2000);
  var out=[];
  lines.forEach(function(ln){
    if(logState.search&&ln.toLowerCase().indexOf(logState.search)<0)return;
    var cls=classify(ln);
    if(logState.level==='error'&&cls!=='err'&&cls!=='del')return;
    if(logState.level==='warn'&&cls!=='warn')return;
    var m=ln.match(/^(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2})([\s\S]*)$/);
    var ts=m?m[1]:'';var tx=m?m[2]:ln;
    out.push('<span class="logline '+cls+'">'+(ts?'<span class="ts">'+esc(ts)+'</span>':'')+'<span class="tx">'+esc(tx)+'</span></span>');
  });
  var box=el('logsBox');
  if(!out.length){box.innerHTML='<div class="list-empty"><span class="ico">📝</span><span class="t">暂无运行日志</span></div>';syncLogBack();return}
  box.innerHTML=out.join('');
  if(logState.follow){box.scrollTop=box.scrollHeight}
  syncLogBack();
}
function loadLogs(){return api('/api/logs?_='+Date.now()).then(function(j){logState.raw=j.logs||'';renderLogs()})}
el('logSearch').addEventListener('input',function(){logState.search=val('logSearch').toLowerCase();renderLogs()});
el('logChips').addEventListener('click',function(e){var c=e.target.closest('.chip');if(!c)return;logState.level=c.dataset.log;el('logChips').querySelectorAll('.chip').forEach(function(x){x.classList.toggle('active',x===c)});renderLogs()});
el('logFollow').addEventListener('change',function(){logState.follow=checked('logFollow');if(logState.follow){el('logsBox').scrollTop=el('logsBox').scrollHeight}syncLogBack()});
el('logsBox').addEventListener('scroll',function(){var b=el('logsBox');var atBottom=(b.scrollHeight-b.scrollTop-b.clientHeight)<8;if(!atBottom&&checked('logFollow')){el('logFollow').checked=false;logState.follow=false;syncLogBack()}});
el('logBackBottom').addEventListener('click',function(){logState.follow=true;el('logFollow').checked=true;el('logsBox').scrollTop=el('logsBox').scrollHeight;syncLogBack()});
function logCopy(){var box=el('logsBox');copyText(box.innerText||box.textContent||'')}

/* ---------- misc ---------- */
function formatSize(b){b=b||0;if(b<1024)return b+' B';if(b<1048576)return (b/1024).toFixed(1)+' KB';if(b<1073741824)return (b/1048576).toFixed(1)+' MB';return (b/1073741824).toFixed(2)+' GB'}

/* ---------- wire events ---------- */
el('saveBtn').addEventListener('click',function(){saveCfg(el('saveBtn')).catch(function(e){toast(e.message,'error')})});
el('saveBtn2').addEventListener('click',function(){saveCfg(el('saveBtn2')).catch(function(e){toast(e.message,'error')})});
el('testBtn').addEventListener('click',testConn);
el('toggleToken').addEventListener('click',function(){var t=el('token');if(t.type==='password'){t.type='text';el('toggleToken').textContent='隐藏'}else{t.type='password';el('toggleToken').textContent='显示'}});
el('scanBtn').addEventListener('click',function(){doScan(false)});
el('cleanBtn').addEventListener('click',function(){doScan(true)});
el('recRefreshRun').addEventListener('click',function(){loadRecords().then(function(){switchTab('logs')}).catch(function(e){toast(e.message,'error')})});
el('openDirPick').addEventListener('click',openDirPicker);
el('clearTasksBtn').addEventListener('click',function(){
  if(taskList().length===0){toast('列表已为空','warn');return}
  confirmDialog({title:'确认清空目录列表？',body:'将移除全部 '+taskList().length+' 个清理目录；不会删除任何网盘文件，但自动清理将无目录可扫。',okText:'清空列表'}).then(function(ok){if(ok){setTaskList([]);setDirty(true);toast('已清空目录列表','success')}});
});
el('copyListBtn').addEventListener('click',copyList);
el('markAllBtn').addEventListener('click',function(){markAll(true);toast('已全部标记为已复核','success')});
el('unmarkAllBtn').addEventListener('click',function(){markAll(false);toast('已取消标记','success')});
el('recordsBtn').addEventListener('click',function(){loadRecords().then(function(){toast('已刷新','success')}).catch(function(e){toast(e.message,'error')})});
el('logsBtn').addEventListener('click',function(){loadLogs().then(function(){toast('已刷新','success')}).catch(function(e){toast(e.message,'error')})});
el('logCopyBtn').addEventListener('click',logCopy);
el('clearLogsBtn').addEventListener('click',function(){
  confirmDialog({title:'确认清空运行日志？',body:'将删除全部运行日志；此操作不可恢复（清理记录不受影响）。',okText:'清空日志'}).then(function(ok){if(ok)api('/api/clear_logs',{method:'POST'}).then(function(){return loadLogs()}).then(function(){toast('日志已清空','success')}).catch(function(e){toast(e.message,'error')})});
});
el('guideToggle').addEventListener('click',function(){var v=el('guideCard').dataset.collapsed!=='1';applyGuideCollapsed(v);try{localStorage.setItem('nds_guideCollapsed',v?'1':'0')}catch(e){}});
// dirty listeners
['address','token','adExts','videoExts','sizeLimit','opsPerSec','cooldown','excludeDirs','pushDebounce','incompleteSuffixes','maxFilesPerRun','maxTotalBytes','burst','maxDepth','forceRefresh','offlineOnly','deletePermanently','allowDelete','enablePush'].forEach(function(id){
  var e=el(id);if(!e)return;e.addEventListener('input',function(){setDirty(true)});e.addEventListener('change',function(){setDirty(true)});
});
// 勾选/取消「事件驱动实时清理」时同步常驻推送权限告警显隐。
el('enablePush').addEventListener('change',renderPushWarn);
window.addEventListener('beforeunload',function(e){if(dirty){e.preventDefault();e.returnValue=''}});

/* ---------- last scan (自动呈现最近结果) ---------- */
function loadLastScan(){
  return api('/api/last_scan?_='+Date.now()).then(function(j){
    if(!j||!j.result)return;
    lastScan=j.result;
    renderStats(j.result);
    renderResult(j.result.items||[]);
    renderOffline(j.result.offlineTasks||[]);
    renderErrors(j.result.errors||[]);
  }).catch(function(){});
}

/* ---------- init ---------- */
function load(){
  return api('/api/state?_='+Date.now()).then(function(j){
    state=j;fillConfig(j.config);
    lastToken=j.status&&j.status.token?j.status.token:null;
    if(lastToken){renderPerms(lastToken);setConn('ok',lastToken)}else{setConn('none')}
    renderStats({checked:0,matched:0,deleted:0,skipped:0});
    el('statChecked').textContent='-';el('statMatched').textContent='-';el('statDeleted').textContent='-';el('statSkipped').textContent='-';
    updateNextAction();
  });
}
switchTab('run');
load().catch(function(e){toast(e.message,'error')});
loadRecords().catch(function(){});
loadLogs().catch(function(){});
loadLastScan();
// 轻量轮询：事件驱动后台触发扫描时，页面自动呈现最新结果（不扫全树，仅拉取内存快照）。
setInterval(loadLastScan,15000);
</script>
</body></html>`
