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
--z-header:100;--z-modal:300;--z-toast:400}
*{box-sizing:border-box}
body{margin:0;background:var(--bg);font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;color:var(--text);font-size:15px;min-height:100vh}
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
.brand h1{font-size:18px;font-weight:700;color:var(--blue)}
.conn{display:flex;align-items:center;gap:6px;font-size:13px;color:var(--muted)}
.dot{width:8px;height:8px;border-radius:999px;background:var(--muted);flex:0 0 auto}
.dot.ok{background:var(--green);box-shadow:0 0 8px rgba(63,185,80,.4)}
.dot.connecting{background:var(--orange)}
.dot.fail{background:var(--red)}
.perms{display:flex;gap:5px;flex-wrap:wrap}
.badge{padding:2px 7px;border-radius:4px;font-size:12px;line-height:1.45}
.badge.ok{background:var(--ok-bg);color:var(--ok-text)}
.badge.bad{background:var(--bad-bg);color:var(--bad-text);cursor:help}
.badge.neutral{background:var(--bg);color:var(--muted)}
.badge.count{background:var(--card);color:var(--muted);border-radius:999px}
.tb-spacer{flex:1 1 auto}
.dirty{font-size:13px;color:var(--warning-text)}
.tb-meta{font-size:13px;color:var(--muted)}
.suggest{color:var(--blue)}
.progress{height:2px;width:100%;background:transparent;overflow:hidden}
.progress.on{background:linear-gradient(90deg,transparent 0,var(--blue) 40%,var(--green) 60%,transparent 100%);background-size:30% 100%;background-repeat:no-repeat;animation:slide 1.1s linear infinite}
@keyframes slide{0%{background-position:-40% 0}100%{background-position:140% 0}}

/* ---------- tabs ---------- */
.tabs{display:flex;gap:4px;padding:0 12px;border-top:1px solid var(--line);overflow-x:auto}
.tab{position:relative;border:0;background:transparent;color:var(--muted);font-size:14px;font-weight:600;padding:11px 13px;cursor:pointer;white-space:nowrap;border-bottom:2px solid transparent}
.tab:hover{color:var(--text)}
.tab.active{color:var(--blue);border-bottom-color:var(--blue)}
.tab .dotmini{display:inline-block;width:6px;height:6px;border-radius:999px;background:var(--warning);margin-left:5px;vertical-align:middle}

/* ---------- layout ---------- */
.main{max-width:1400px;margin:0 auto;padding:var(--s4)}
.grid{display:grid;grid-template-columns:1fr 380px;gap:var(--s4)}
.grid2{display:grid;grid-template-columns:1fr 1fr;gap:var(--s4);align-items:stretch}
/* .col 用 flex 列布局 + 卡片 flex:1：让同一行两张卡片等高，
   消除「清理目录」比「CD2 连接」短一截时露出的豁口（不整齐问题）。 */
.col{display:flex;flex-direction:column;gap:var(--s4)}
.col>.card{flex:1 1 auto;display:flex;flex-direction:column}
.col>.card>.panel{flex:1 1 auto}
section.tabpane{display:none}
/* 页内所有块统一 16px 间距：此前全宽卡片紧贴上一行（0 间距），与网格内 16px 不一致。 */
section.tabpane.active{display:grid;gap:var(--s4);align-content:start}
.card{background:var(--panel);border:1px solid var(--line);border-radius:var(--r-xl);padding:18px}
.card h2{font-size:16px;font-weight:600;color:var(--text-dim);margin-bottom:12px;display:flex;align-items:center;gap:8px}
.card h2 .spacer{flex:1}
.card h2 .step{color:var(--muted);font-size:13px}
.sub{color:var(--muted);font-size:13px;margin:-6px 0 12px}

/* ---------- buttons ---------- */
.btn{border:0;border-radius:var(--r-md);padding:9px 13px;font-size:14px;font-weight:600;cursor:pointer;transition:.15s;display:inline-flex;align-items:center;gap:6px;line-height:1}
.btn:focus-visible{outline:2px solid var(--info);outline-offset:2px}
.btn:disabled{opacity:.45;cursor:not-allowed}
.btn-primary{background:var(--success-bg);color:#fff}.btn-primary:hover:not(:disabled){background:var(--success-hover)}
.btn-ok{background:var(--info-bg);color:#fff}.btn-ok:hover:not(:disabled){background:var(--info-hover)}
.btn-danger{background:var(--danger-bg);color:#fff}.btn-danger:hover:not(:disabled){background:var(--danger)}
.btn-ghost{background:var(--bg);border:1px solid var(--line);color:var(--text)}.btn-ghost:hover:not(:disabled){background:var(--card)}
.btn-sm{padding:5px 10px;font-size:13px}
.actions{display:flex;gap:var(--s2);flex-wrap:wrap;margin-top:var(--s3)}
.spinner{width:14px;height:14px;border-radius:999px;border:2px solid rgba(255,255,255,.35);border-top-color:#fff;animation:spin .7s linear infinite;display:inline-block}
@keyframes spin{to{transform:rotate(360deg)}}

/* ---------- forms ---------- */
.formgroup{margin-bottom:var(--s3)}
.formgroup label{display:block;margin-bottom:5px;color:var(--muted);font-size:13px;font-weight:500}
.formgroup input,.formgroup textarea,.formgroup select{width:100%;border:1px solid var(--line);background:var(--bg);color:var(--text);border-radius:var(--r-md);padding:9px 11px;outline:none;font-size:14px;transition:.15s;height:38px}
.formgroup textarea{height:auto;min-height:60px;resize:vertical}
.formgroup input:focus,.formgroup textarea:focus,.formgroup select:focus{border-color:var(--blue);box-shadow:0 0 0 3px rgba(88,166,255,.1)}
.formgroup .help{font-size:12px;color:var(--muted);margin-top:4px}
.formgroup .help.warn{color:var(--warning-text)}
.checks .help.warn{margin-top:6px;padding:8px 12px;font-size:13px;line-height:1.5;color:var(--warning-text);background:var(--warning-bg);border:1px solid var(--warning);border-radius:6px}
.row2{display:grid;grid-template-columns:1fr 1fr;gap:var(--s3)}.row3{display:grid;grid-template-columns:1fr 1fr 1fr;gap:var(--s3)}
.checks{display:flex;gap:var(--s2);flex-wrap:wrap;margin-top:var(--s3)}
.check{display:flex;align-items:center;gap:6px;border:1px solid var(--line);border-radius:var(--r-sm);padding:7px 10px;background:var(--bg);font-size:13px;color:var(--text)}
.check.warn{border-color:var(--danger)}
.check input{width:auto;height:auto}
.inline{display:flex;gap:6px;align-items:center}
.inline input{flex:1}

/* ---------- details (advanced) ---------- */
details.adv{margin-top:var(--s3);border:1px solid var(--line);border-radius:var(--r-md);background:var(--bg)}
details.adv > summary{cursor:pointer;padding:11px 13px;font-size:14px;font-weight:600;color:var(--text-dim);list-style:none}
details.adv > summary::-webkit-details-marker{display:none}
details.adv > summary:before{content:"▸ ";color:var(--muted)}
details.adv[open] > summary:before{content:"▾ "}
details.adv .adv-body{padding:0 12px 12px}

/* ---------- push state ---------- */
.pushtag{font-weight:600}
.pushbox{margin-top:var(--s3)}

/* ---------- lists ---------- */
.panel{max-height:320px;overflow:auto;border:1px solid var(--line);border-radius:var(--r-md);background:var(--bg)}
.panel::-webkit-scrollbar{width:6px;height:6px}.panel::-webkit-scrollbar-thumb{background:var(--line);border-radius:999px}
.list-empty{padding:var(--s5) 12px;text-align:center;color:var(--muted)}
.list-empty .ico{font-size:32px;display:block;margin-bottom:var(--s2)}
.list-empty .t{color:var(--text-dim);font-size:15px}
.taskitem{display:flex;align-items:center;gap:var(--s2);padding:9px 11px;border-bottom:1px solid #1c2128;font-size:14px}
.taskitem:last-child{border-bottom:0}
.taskitem .path{flex:1;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;color:var(--text-dim)}
.mini{border:1px solid var(--line);background:var(--bg);color:var(--muted);border-radius:var(--r-sm);cursor:pointer;font-size:13px;padding:4px 8px;line-height:1}
.mini:hover:not(:disabled){color:var(--text);background:var(--card)}
.mini:disabled{opacity:.3;cursor:not-allowed}
.mini.del:hover{color:var(--danger-text);border-color:var(--danger)}
.diritem{display:flex;align-items:center;gap:var(--s2);padding:8px 10px;border-bottom:1px solid #1c2128;cursor:pointer}
.diritem:hover{background:var(--card)}
.diritem:last-child{border-bottom:0}
.diritem .name{flex:1;font-size:14px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.diritem .added{color:var(--success-text);font-size:13px}
.diritem input[type=checkbox]{width:auto;height:auto}
.crumbs{display:flex;gap:4px;flex-wrap:wrap;align-items:center;font-size:14px;margin-bottom:var(--s2);color:var(--muted)}
.crumbs .c{cursor:pointer;color:var(--blue)}
.crumbs .c:hover{text-decoration:underline}
.crumbs .cur{color:var(--text);font-weight:600}
.crumbs .sep{color:var(--muted)}

/* ---------- log toolbar / chips ---------- */
.tblbar{display:flex;gap:var(--s2);flex-wrap:wrap;align-items:center;margin-bottom:var(--s2)}
.tblbar input[type=text],.tblbar select{background:var(--bg);border:1px solid var(--line);color:var(--text);border-radius:var(--r-md);padding:7px 10px;font-size:13px;height:36px}
.tblbar input[type=text]{flex:1;min-width:140px}
.chips{display:flex;gap:4px;flex-wrap:wrap}
.chip{border:1px solid var(--line);background:var(--bg);color:var(--muted);border-radius:999px;font-size:12px;padding:4px 10px;cursor:pointer}
.chip.active{background:var(--card);color:var(--text);border-color:var(--blue)}

/* ---------- logs ---------- */
.logbox{max-height:420px;overflow:auto;padding:12px;background:var(--log-bg);border-radius:var(--r-md);border:1px solid var(--line);font-size:13px;line-height:1.6;color:var(--muted);white-space:pre-wrap;font-family:ui-monospace,Menlo,Consolas,monospace}
.logline{display:block}
.logline .ts{color:var(--muted)}
.logline .tx{color:var(--text-dim)}
.logline.err .tx{color:var(--danger-text)}
.logline.warn .tx{color:var(--warning-text)}
.logline.del .tx{color:var(--danger-text);font-weight:700}
.backlatest{position:sticky;bottom:8px;display:inline-block;background:var(--info-bg);color:#fff;border:0;border-radius:999px;padding:6px 13px;font-size:13px;cursor:pointer}

/* ---------- banners / empty / skeleton ---------- */
.banner{padding:10px 13px;border-radius:var(--r-md);font-size:13px;line-height:1.6;margin-bottom:var(--s3);border:1px solid}
.banner-warn{background:var(--warning-bg);border-color:var(--warning);color:var(--warning-text)}
.banner-danger{background:var(--bad-bg);border-color:var(--danger);color:var(--danger-text)}
.banner-info{background:rgba(88,166,255,.08);border-color:var(--info);color:var(--blue)}
.skel{display:inline-block;min-width:44px;height:12px;border-radius:4px;background:linear-gradient(90deg,var(--card),#2b313a,var(--card));background-size:200% 100%;animation:sk 1.2s linear infinite;vertical-align:middle}
@keyframes sk{0%{background-position:200% 0}100%{background-position:-200% 0}}

/* ---------- toast / modal ---------- */
#toastRoot{position:fixed;right:18px;bottom:18px;z-index:var(--z-toast);display:flex;flex-direction:column;gap:8px;align-items:flex-end}
.toast{border-radius:var(--r-lg);padding:11px 15px;font-size:14px;box-shadow:0 8px 24px rgba(0,0,0,.4);max-width:360px;border:1px solid;cursor:pointer}
.toast.success{background:var(--ok-bg);color:var(--ok-text);border-color:#26a641}
.toast.error{background:var(--bad-bg);color:var(--bad-text);border-color:var(--danger-bg)}
.toast.info{background:var(--card);color:var(--text);border-color:var(--line)}
.toast.warn{background:var(--warning-bg);color:var(--warning-text);border-color:var(--warning)}
#modalRoot{position:fixed;inset:0;z-index:var(--z-modal);display:none}
.modal-mask{position:absolute;inset:0;background:rgba(1,4,9,.72);display:flex;align-items:center;justify-content:center;padding:16px;animation:fade .12s ease}
@keyframes fade{from{opacity:0}to{opacity:1}}
.modal{width:440px;max-width:100%;background:var(--panel);border:1px solid var(--line);border-radius:var(--r-xl);box-shadow:0 16px 48px rgba(0,0,0,.55);padding:18px;animation:rise .12s ease}
@keyframes rise{from{transform:translateY(8px);opacity:0}to{transform:none;opacity:1}}
.modal-title{font-size:16px;font-weight:700;color:var(--text);margin-bottom:10px}
.modal-body{font-size:14px;color:var(--text-dim);line-height:1.7}
.modal-list{margin:6px 0;padding-left:18px}
.modal-list li{margin:2px 0}
.modal-input{width:100%;margin-top:10px;background:var(--bg);border:1px solid var(--line);color:var(--text);border-radius:var(--r-md);padding:9px 11px;font-size:14px;height:38px}
.modal-actions{display:flex;justify-content:flex-end;gap:var(--s2);margin-top:var(--s4)}
.modal.wide{width:640px}
.dirpick{max-height:320px;overflow:auto;border:1px solid var(--line);border-radius:var(--r-md);background:var(--bg)}
.dirpick .diritem:last-child{border-bottom:0}

/* ---------- responsive ---------- */
@media(max-width:1199px){.grid,.grid2{grid-template-columns:1fr}}
@media(max-width:767px){
 .main{padding:12px}.topbar{padding:8px 12px}
 .row2,.row3{grid-template-columns:1fr}
 .btn{min-height:44px}
 .tab{padding:12px 10px}
 .mini,.chip{min-height:44px;display:inline-flex;align-items:center}
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
    </div>
    <div class="tb-row">
      <span class="tb-meta">Token 根目录: <span id="tokenRoot">-</span></span>
      <span class="tb-meta" id="runState">空闲</span>
      <span class="tb-meta" id="lastRunMeta">-</span>
      <span class="tb-meta">事件驱动: <span class="pushtag" id="pushState">-</span></span>
      <div class="tb-spacer"></div>
      <span class="tb-meta suggest" id="nextAction"></span>
    </div>
    <div class="progress" id="progress"></div>
  </div>
  <nav class="tabs" id="tabs">
    <button class="tab" data-tab="config">① 连接 · 目录 · 规则<span class="dotmini hidden" id="tabRulesDot"></span><span class="badge count hidden" id="tabConnCount"></span></button>
    <button class="tab active" data-tab="logs">② 运行日志</button>
  </nav>
</div>

<main class="main">
  <!-- ① 连接 · 目录 · 规则（合并原「连接与目录」「清理规则」两页） -->
  <section class="tabpane" id="tab-config">
    <div class="grid2">
      <div class="col">
        <div class="card">
          <h2>CD2 连接</h2>
          <div class="row2">
            <div class="formgroup"><label>gRPC 地址</label><input id="address" placeholder="127.0.0.1:19798"><div class="help">CD2 的 gRPC 端口，默认 127.0.0.1:19798</div></div>
            <div class="formgroup"><label>API Token</label>
              <div class="inline"><input id="token" type="password" autocomplete="off"><button type="button" class="btn btn-ghost btn-sm" id="toggleToken">显示</button></div>
              <div class="help">在 CD2 中创建，需含 allow_list / allow_delete（清理时）/ allow_push_message（事件驱动时）</div>
            </div>
          </div>
          <div class="actions">
            <button class="btn btn-ok" id="testBtn">测试连接</button>
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

    <div class="card">
      <h2>清理规则</h2>
      <div class="row2">
        <div class="formgroup"><label>广告后缀</label><input id="adExts" value=".txt,.html,.url,.lnk"><div class="help">命中即判为垃圾（逗号分隔）</div></div>
        <div class="formgroup"><label>视频后缀</label><input id="videoExts" value=".mp4,.mkv,.ts"><div class="help">配合下方阈值按体积判定</div></div>
        <div class="formgroup"><label>小视频阈值 MB</label><input id="sizeLimit" type="number" step="0.1" value="20"><div class="help">视频后缀且体积 ≤ 此值即命中。默认 20 MB</div></div>
        <div class="formgroup"><label>限速 ops/秒</label><input id="opsPerSec" type="number" step="0.1" value="5"><div class="help warn">每次 gRPC 调用前取令牌。默认 5，对齐 115 官方上限，调高会增加风控风险</div></div>
        <div class="formgroup"><label>文件冷却小时</label><input id="cooldown" type="number" value="0"><div class="help">0 = 立即清理（发现即删，推荐）；&gt;0 则新文件（含刚完成的离线下载）在冷却期内跳过。默认 0</div></div>
        <div class="formgroup"><label>排除关键词</label><input id="excludeDirs" value="重要,备份"><div class="help">目录名包含任一关键词即整目录跳过。重要目录务必填入</div></div>
        <div class="formgroup"><label>推送防抖秒数</label><input id="pushDebounce" type="number" value="5"><div class="help">事件驱动下合并突发变更的静默窗口。默认 5 秒（保存配置后即时生效）</div></div>
        <div class="formgroup"><label>未完成后缀</label><input id="incompleteSuffixes" value=".part,.download,.!qB,.bc!,.aria2,.crdownload,.td,.tmp,.!ut"><div class="help">含这些后缀的目录整目录跳过。留空会自动回填默认值，不建议清空</div></div>
      </div>
      <div class="checks">
        <label class="check warn"><input id="forceRefresh" type="checkbox">强制刷新 CD2 缓存（会显著增加网盘 API 压力，非必要不建议开启）</label>
        <label class="check"><input id="offlineOnly" type="checkbox" checked>只清理已完成离线任务</label>
        <label class="check"><input id="deletePermanently" type="checkbox">永久删除（不进回收站）</label>
        <label class="check warn"><input id="allowDelete" type="checkbox">允许自动清理（删除总开关）</label>
        <label class="check"><input id="enablePush" type="checkbox" checked>启用事件驱动实时清理（PushMessage）</label>
      </div>
      <div class="pushbox" id="pushHint"></div>
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

    <div class="card">
      <h2>保存配置</h2>
      <div class="sub">修改连接、目录或规则后点下方按钮保存。保存后事件驱动订阅会按新配置自动重启，无需重启容器。</div>
      <div class="actions"><button class="btn btn-primary" id="saveBtn2">保存配置</button></div>
    </div>
  </section>

  <!-- ② 运行日志（唯一结果视图；系统运行 / 事件驱动实时清理 / 手动清理日志都汇总在此）-->
  <section class="tabpane active" id="tab-logs">
    <div class="card">
      <h2>运行日志<span class="spacer"></span><button class="btn btn-primary btn-sm" id="runBtn" title="按当前规则扫描并清理，结果写入下方日志">手动清理</button><button class="btn btn-ghost btn-sm" id="logCopyBtn">复制</button><button class="btn btn-ghost btn-sm" id="logsBtn">刷新</button><button class="btn btn-danger btn-sm" id="clearLogsBtn">清空</button></h2>
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
  </section>
</main>

<div id="modalRoot"></div>
<div id="toastRoot"></div>
<input id="tasksHidden" type="hidden">
<script>
var state={},dirty=false,lastScan=null,lastScanTime='',lastToken=null,savedOnce=false;
var lastPush={state:'off',detail:'',events:0,lastEvent:''};
var lastStatus=null; // 最近的 /api/state|/api/push 运行状态（含 cloudApis 云端事件监听器状态）
var logState={level:'all',search:'',follow:true,raw:''};
var activeTab='logs';

function el(id){return document.getElementById(id)}
function val(id){var e=el(id);return e?e.value:''}
function setv(id,v){el(id).value=(v===undefined||v===null)?'':v}
function checked(id){var e=el(id);return e?e.checked:false}
function esc(s){return String(s===undefined||s===null?'':s).replace(/[&<>"']/g,function(m){return {'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[m]})}

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
function setDirty(v){dirty=v;el('dirtyFlag').classList.toggle('hidden',!v);el('tabRulesDot').classList.toggle('hidden',!v);}

/* ---------- run feedback（无秒级计时、无全屏遮罩） ---------- */
// 需求：手动清理时不再显示秒级耗时文案，改为在「运行日志」里实时滚动清理明细。
// 进行中反馈仅保留 #runBtn 的内联 spinner + 禁用态与顶部进度条，不再用全屏遮罩遮挡日志。
var runPollTimer=null;
function startRun(btn,label){
  if(btn){btn.dataset.orig=btn.textContent;btn.disabled=true;btn.innerHTML='<span class="spinner"></span>'+esc(label)}
  el('progress').classList.add('on');
  el('runState').textContent='运行中…';
  el('runBtn').disabled=true;
}
function endRun(){
  el('progress').classList.remove('on');el('runState').textContent='空闲';
  var b=el('runBtn');b.disabled=false;if(b.dataset.orig){b.textContent=b.dataset.orig;delete b.dataset.orig}
  guardEmptyTasks();
}
// 运行期高频拉取日志：切到日志页、强制跟随最新、先刷一次，再每 1200ms 拉一次。
// 期间让 8s 常规轮询让位（常规轮询判断 runPollTimer 存在则跳过），避免重复请求。
function startRunPolling(){
  logState.follow=true;el('logFollow').checked=true;
  switchTab('logs');
  loadLogs().catch(function(){});
  if(runPollTimer)clearInterval(runPollTimer);
  runPollTimer=setInterval(function(){loadLogs().catch(function(){})},1200);
}
// 结束运行期滚动：清掉定时器，并立即补刷一次，确保收尾日志（扫描汇总）也在页面上。
function stopRunPolling(){
  if(runPollTimer){clearInterval(runPollTimer);runPollTimer=null}
  loadLogs().catch(function(){});
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
// 权限徽章：显示名用中文（用户要求），悬浮提示保留 CD2 的 proto 字段名便于到 CD2 侧对照勾选。
var PERM=[
  ['列目录','allowList','allow_list','无法读取目录'],
  ['回收站删除','allowDelete','allow_delete','无法删除到回收站'],
  ['永久删除','allowDeletePermanently','allow_delete_permanently','无法永久删除'],
  ['消息推送','allowPushMessage','allow_push_message','事件驱动实时清理不会生效']
];
function renderPerms(info){
  var box=el('permBadges');
  if(info&&info.rootDir)el('tokenRoot').textContent=info.rootDir;
  if(!info){box.innerHTML='<span class="badge neutral">权限未读取</span>';return}
  box.innerHTML=PERM.map(function(p){
    var has=!!info[p[1]];
    var tip=has?('已授予 '+p[2]):('缺少 '+p[2]+' → '+p[3]);
    return '<span class="badge '+(has?'ok':'bad')+'" title="'+esc(tip)+'">'+p[0]+'</span>';
  }).join('');
}
// 事件驱动状态：不再由前端「猜」权限，而是直接展示后端的真实订阅状态（问题 1 的根因修复）。
// 此前用 lastToken.allowPushMessage 在前端推断并常驻告警，token 状态稍一陈旧就会误报
// 「缺少 allow_push_message」，甚至与徽章显示自相矛盾。现在唯一可信来源是后端。
var PUSH_LABEL={off:'已停止',config_missing:'未启用',connecting:'连接中…',running:'运行中',denied:'权限不足',error:'连接失败'};
var PUSH_COLOR={off:'var(--muted)',config_missing:'var(--warning-text)',connecting:'var(--warning-text)',running:'var(--success-text)',denied:'var(--danger-text)',error:'var(--danger-text)'};
/* 存活可观测性（D5）时间辅助：后端时间戳为 "YYYY-MM-DD HH:MM:SS"（本地时间）。 */
function parseTS(s){if(!s)return 0;var t=Date.parse(String(s).replace(' ','T'));return isNaN(t)?0:t}
function agoText(s){var t=parseTS(s);if(!t)return '';var d=Date.now()-t;if(d<0)d=0;var m=Math.floor(d/60000);if(m<1)return '刚刚';if(m<60)return m+' 分钟前';var h=Math.floor(m/60);if(h<48)return h+' 小时前';return Math.floor(h/24)+' 天前'}
function durText(s){var t=parseTS(s);if(!t)return '';var d=Date.now()-t;if(d<0)d=0;var m=Math.floor(d/60000);if(m<60)return m+' 分钟';var h=Math.floor(m/60);if(h<48)return h+' 小时';return Math.floor(h/24)+' 天'}
var PUSH_STALE_MS=30*60*1000; // 运行中但超过 30 分钟无任何推送 → 黄色提示（给用户的自证手段）
function renderPush(p){
  if(p)lastPush=p;
  var tag=el('pushState');
  tag.textContent=PUSH_LABEL[lastPush.state]||lastPush.state||'-';
  tag.style.color=PUSH_COLOR[lastPush.state]||'var(--muted)';
  tag.title=lastPush.detail||'';
  var st=lastPush.state,html='';
  if(st==='running'){
    var meta=[];
    if(lastPush.gen)meta.push('世代 '+lastPush.gen);
    if(lastPush.subscribedAt)meta.push('已建立 '+durText(lastPush.subscribedAt));
    meta.push(lastPush.lastMessageAt?('最近收到推送 '+agoText(lastPush.lastMessageAt)):'尚未收到任何推送');
    if(lastPush.reconnects)meta.push('重连 '+lastPush.reconnects+' 次');
    var live='';
    if(lastPush.lastEventPath)live+='（最近变更 '+esc(lastPush.lastEventPath)+'）';
    html='<div class="banner banner-info">事件驱动实时清理<b>运行中</b>：'+esc(lastPush.detail||'')+'；已收到 '+(lastPush.events||0)+' 个文件变更事件'+(lastPush.lastEvent?('，最近 '+esc(lastPush.lastEvent)):'')+live+'。<br><span style="opacity:.75">'+esc(meta.join(' · '))+'</span></div>';
    var anchor=parseTS(lastPush.lastMessageAt)||parseTS(lastPush.subscribedAt);
    if(anchor&&(Date.now()-anchor)>PUSH_STALE_MS){
      html+='<div class="banner banner-warn">订阅存活但已超过 30 分钟未收到任何推送；若期间有文件变更未被清理，请检查 CD2 云端事件监听器（isCloudEventListenerRunning）。</div>';
    }
  }else if(st==='denied'){
    html='<div class="banner banner-danger">事件驱动实时清理<b>未生效</b>：'+esc(lastPush.detail||'')+'。请在 CD2 为该 Token 勾选 allow_push_message，然后回本页点「保存配置」（无需重启容器）。</div>';
  }else if(st==='error'){
    html='<div class="banner banner-warn">事件驱动实时清理异常：'+esc(lastPush.detail||'')+'。</div>';
  }else if(st==='config_missing'){
    html='<div class="banner banner-warn">事件驱动实时清理未启动：'+esc(lastPush.detail||'')+'。保存配置后会自动启动，无需重启容器。</div>';
  }else if(st==='connecting'){
    html='<div class="banner banner-warn">事件驱动实时清理正在连接 CD2…</div>';
  }else if(st==='off'){
    html='<div class="banner banner-warn">事件驱动实时清理已停止：'+esc(lastPush.detail||'')+'。保存配置或等待监督器自动重建。</div>';
  }
  // 非运行中时必须暴露最近一次订阅失败原因（D5，已脱敏，绝不写 Token 明文）。
  if(st!=='running'&&lastPush.lastError){
    html+='<div class="banner banner-warn">最近一次订阅失败：'+esc(lastPush.lastError)+(lastPush.lastErrorAt?('（'+esc(lastPush.lastErrorAt)+'）'):'')+'。</div>';
  }
  // 云端事件监听器告警：仅在拿到数据且确有掉线云盘时提示（拿不到时静默降级，不误报）。
  if(lastStatus&&lastStatus.cloudApis&&lastStatus.cloudApis.length){
    var down=lastStatus.cloudApis.filter(function(a){return a.isCloudEventListenerRunning===false});
    if(down.length){
      html+='<div class="banner banner-danger">警告：CD2 云盘「'+down.map(function(a){return esc(a.name)}).join('、')+'」的云端事件监听器未运行，CD2 将不再推送文件变更事件，事件驱动清理不会触发；请在 CD2 中检查该云盘连接/重新登录。</div>';
    }
  }
  el('pushHint').innerHTML=html;
}
function updateNextAction(){
  var a='';
  if(!lastToken)a='建议：先到「① 连接 · 目录 · 规则」测试连接';
  else if(taskList().length<1)a='建议：添加至少 1 个清理目录';
  else if(!lastScan)a='建议：到「② 运行日志」点「手动清理」执行一次';
  else if(!checked('allowDelete'))a='当前为预览模式：勾选「允许自动清理」后才会真正删除';
  else a='一切就绪，正在按规则运行';
  el('nextAction').textContent=a;
}

/* ---------- tabs ---------- */
function switchTab(name){
  activeTab=name;
  document.querySelectorAll('.tab').forEach(function(t){t.classList.toggle('active',t.dataset.tab===name)});
  document.querySelectorAll('section.tabpane').forEach(function(s){s.classList.toggle('active',s.id==='tab-'+name)});
  window.scrollTo({top:0,behavior:'smooth'});
}
el('tabs').addEventListener('click',function(e){var b=e.target.closest('.tab');if(b)switchTab(b.dataset.tab)});

/* ---------- tasks ---------- */
function taskList(){var v=val('tasksHidden');if(!v)return[];return v.split('\n').map(function(x){return x.trim()}).filter(Boolean)}
function setTaskList(list){setv('tasksHidden',list.join('\n'));renderTaskList(list);updateNextAction();guardEmptyTasks()}
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
function guardEmptyTasks(){var empty=taskList().length===0;var b=el('runBtn');b.disabled=empty;b.title=empty?'尚未配置任何清理目录：请先到「① 连接 · 目录 · 规则」添加目录':'手动清理（未开启「允许自动清理」时只扫描不删除）';}

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
  setDirty(false);updateNextAction();
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
    savedOnce=true;
    setDirty(false);toast('配置已保存','success');
    // 保存会触发后端热重启事件驱动订阅；稍后刷新状态，让「运行中/权限不足」立刻可见。
    setTimeout(function(){loadPush().catch(function(){})},600);
    return j;
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
      updateNextAction();
      toast(j.message,'success');
    }).catch(function(e){setConn('fail',null,e.message);toast(e.message,'error')}).finally(function(){
      // §9.9 ③：测试期间按钮 disabled + 「测试中…」，成功/失败都要恢复。
      btn.disabled=false;if(btn.dataset.orig){btn.textContent=btn.dataset.orig;delete btn.dataset.orig}
    });
  };
  if(dirty){confirmDialog({title:'测试前需保存配置',body:'检测到未保存的修改，测试连接需要先保存当前配置，是否继续？',okText:'保存并测试'}).then(function(ok){if(ok)go()});}
  else go();
}

/* ---------- 手动清理（扫描与删除合并为单一动作）---------- */
function validTime(v){
  if(!v)return null;
  var d=(v instanceof Date)?v:new Date(v);
  // 后端零值时间会被序列化成 0001-01-01T00:00:00Z，必须挡掉，否则页面显示「公元 1 年」。
  if(isNaN(d.getTime())||d.getFullYear()<2000)return null;
  return d;
}
function fmtTime(v){
  var d=validTime(v);
  if(!d)return '';
  function p(n){return (n<10?'0':'')+n}
  return d.getFullYear()+'-'+p(d.getMonth()+1)+'-'+p(d.getDate())+' '+p(d.getHours())+':'+p(d.getMinutes())+':'+p(d.getSeconds());
}
function renderRunMeta(){
  var when=lastScanTime?('最近一次：'+fmtTime(lastScanTime)):'尚未执行';
  var mode='';
  if(lastScan&&lastScan.deleteMode){
    mode=lastScan.deleteMode==='permanent'?'永久删除':(lastScan.deleteMode==='recycle'?'回收站删除':'仅预览（未删除）');
  }
  var txt=when+(mode?(' · '+mode):'');
  if(!checked('allowDelete'))txt+=' · 只扫不删';
  el('lastRunMeta').textContent=txt;
}
function doRun(){
  if(taskList().length===0){toast('请先添加清理目录','error');switchTab('config');return}
  // 未开启删除总开关 → 只扫描不删除，不弹删除确认（避免「说要删却什么都没删」的困惑）。
  if(!checked('allowDelete')){executeRun(false);return}
  var permanent=checked('deletePermanently');
  var body='<ul class="modal-list">'
    +'<li>将按当前规则手动清理，<b>删除</b>命中的垃圾文件</li>'
    +'<li>删除方式：'+(permanent?'<b class="danger-text">永久删除，不进回收站，不可恢复</b>':'进网盘回收站（可恢复）')+'</li>'
    +'<li>涉及 '+taskList().length+' 个目录</li>'
    +'<li>单轮上限 '+esc(val('maxFilesPerRun'))+' 个 / '+esc(val('maxTotalBytes'))+' GiB</li></ul>'
    +'<div class="banner banner-warn">扫描与删除在同一轮完成：点「确认」后立即按规则删除，不再有二次确认。</div>';
  if(permanent)body+='<div class="banner banner-danger">永久删除不可恢复，请谨慎确认。</div>';
  confirmDialog({title:permanent?'确认手动清理并永久删除？':'确认手动清理？',bodyHtml:body,okText:permanent?'永久删除':'开始清理',confirmWord:permanent?'DELETE':null}).then(function(ok){if(ok)executeRun(true)});
}
function executeRun(doDelete){
  var go=function(){
    var btn=el('runBtn');
    startRun(btn,doDelete?'清理中…':'扫描中…');
    // 运行期启动日志实时滚动（切到日志页并每 1200ms 刷新），让逐行清理明细立刻可见。
    startRunPolling();
    api(doDelete?'/api/clean':'/api/scan').then(function(j){
      lastScan=j;lastScanTime=new Date();
      renderRunMeta();
      toast(doDelete?('手动清理完成：删除 '+j.deleted+' 个'):('手动清理完成（未开启删除总开关，仅扫描）：命中 '+j.matched+' 个'),'success');
    }).catch(function(e){toast(e.message,'error')}).finally(function(){stopRunPolling();endRun()});
  };
  // 未保存的修改不会被本次扫描采用（扫描读的是后端已保存的配置），先保存再执行。
  if(dirty){
    confirmDialog({title:'执行前需保存配置',body:'检测到未保存的修改，扫描只会按「已保存」的配置执行。是否先保存再执行？',okText:'保存并执行'}).then(function(ok){
      if(ok)saveCfg().then(go).catch(function(e){toast(e.message,'error')});
    });
    return;
  }
  go();
}

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
el('saveBtn2').addEventListener('click',function(){saveCfg(el('saveBtn2')).catch(function(e){toast(e.message,'error')})});
el('testBtn').addEventListener('click',testConn);
el('toggleToken').addEventListener('click',function(){var t=el('token');if(t.type==='password'){t.type='text';el('toggleToken').textContent='隐藏'}else{t.type='password';el('toggleToken').textContent='显示'}});
el('runBtn').addEventListener('click',doRun);
el('openDirPick').addEventListener('click',openDirPicker);
el('clearTasksBtn').addEventListener('click',function(){
  if(taskList().length===0){toast('列表已为空','warn');return}
  confirmDialog({title:'确认清空目录列表？',body:'将移除全部 '+taskList().length+' 个清理目录；不会删除任何网盘文件，但自动清理将无目录可扫。',okText:'清空列表'}).then(function(ok){if(ok){setTaskList([]);setDirty(true);toast('已清空目录列表','success')}});
});
el('logsBtn').addEventListener('click',function(){loadLogs().then(function(){toast('已刷新','success')}).catch(function(e){toast(e.message,'error')})});
el('logCopyBtn').addEventListener('click',logCopy);
el('clearLogsBtn').addEventListener('click',function(){
  confirmDialog({title:'确认清空运行日志？',body:'将删除全部运行日志；此操作不可恢复（清理记录不受影响）。',okText:'清空日志'}).then(function(ok){if(ok)api('/api/clear_logs',{method:'POST'}).then(function(){return loadLogs()}).then(function(){toast('日志已清空','success')}).catch(function(e){toast(e.message,'error')})});
});
// dirty listeners
['address','token','adExts','videoExts','sizeLimit','opsPerSec','cooldown','excludeDirs','pushDebounce','incompleteSuffixes','maxFilesPerRun','maxTotalBytes','burst','maxDepth','forceRefresh','offlineOnly','deletePermanently','allowDelete','enablePush'].forEach(function(id){
  var e=el(id);if(!e)return;e.addEventListener('input',function(){setDirty(true)});e.addEventListener('change',function(){setDirty(true)});
});
// 勾选/取消「事件驱动实时清理」：立即刷新提示（后端订阅在保存配置后才真正启停）。
el('enablePush').addEventListener('change',function(){renderPush()});
// 删除总开关/永久删除变化会改变「手动清理」的语义与结果摘要，立即刷新。
el('allowDelete').addEventListener('change',function(){renderRunMeta();updateNextAction()});
el('deletePermanently').addEventListener('change',function(){renderRunMeta()});
window.addEventListener('beforeunload',function(e){if(dirty){e.preventDefault();e.returnValue=''}});

/* ---------- push 状态 / 最近结果（自动呈现后台动作）---------- */
function loadPush(){
  return api('/api/push?_='+Date.now()).then(function(j){
    if(j.status)lastStatus=j.status;
    renderPush(j.push);
    // 运行状态（含 Token 权限）由后端在「启动自检 / 保存自检 / 订阅成功」时写入，
    // 前端据此自动点亮连接状态与权限徽章——用户不必再手点一次「测试连接」。
    if(j.status){
      lastToken=j.status.token||null;
      if(lastToken){renderPerms(lastToken);setConn('ok',lastToken)}
      else{
        var m=String(j.status.last_message||'');
        if(m.indexOf('未连接')===0){renderPerms(null);setConn('fail',null,m.replace(/^未连接:\s*/,''))}
        else{renderPerms(null);setConn('none')}
      }
    }
  });
}
function loadLastScan(){
  return api('/api/last_scan?_='+Date.now()).then(function(j){
    if(!j||!j.result)return;
    lastScan=j.result;
    var t=validTime(j.at);if(t)lastScanTime=t;
    renderRunMeta();
  }).catch(function(){});
}

/* ---------- init ---------- */
function load(){
  return api('/api/state?_='+Date.now()).then(function(j){
    state=j;fillConfig(j.config);
    var t=validTime(j.lastScanAt);if(t)lastScanTime=t;
    lastStatus=j.status||null;
    renderPush(j.push);
    lastToken=j.status&&j.status.token?j.status.token:null;
    if(lastToken){renderPerms(lastToken);setConn('ok',lastToken)}else{renderPerms(null);setConn('none')}
    renderRunMeta();
    updateNextAction();
  });
}
switchTab('logs');
load().catch(function(e){toast(e.message,'error')});
loadLogs().catch(function(){});
loadLastScan();
// 轻量轮询：推送状态与结果快照都只读后端内存（无网络调用，不触发扫全树）。
setInterval(function(){loadPush().catch(function(){})},5000);
// 日志是唯一结果视图：仅在「当前在日志页 且 勾选了跟随最新」时自动刷新，避免打断手动翻阅。
// 运行期（runPollTimer 存在）由 1200ms 高频轮询负责，常规 8s 轮询让位，避免重复请求。
setInterval(function(){if(activeTab==='logs'&&logState.follow&&!runPollTimer)loadLogs().catch(function(){})},8000);
setInterval(loadLastScan,12000);
</script>
</body></html>`
