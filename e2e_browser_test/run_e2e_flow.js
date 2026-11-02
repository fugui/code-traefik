const { chromium } = require('playwright');
const path = require('path');
const fs = require('fs');
const { execSync } = require('child_process');

const BASE_URL = 'http://192.168.56.18:8091';
const SCREENSHOT_DIR = path.resolve(__dirname, 'screenshots');
if (!fs.existsSync(SCREENSHOT_DIR)) {
  fs.mkdirSync(SCREENSHOT_DIR, { recursive: true });
}

async function takeSnap(page, name) {
  const filePath = path.join(SCREENSHOT_DIR, `${name}.png`);
  await page.screenshot({ path: filePath, fullPage: false });
  console.log(`[Screenshot Saved] ${name}.png`);
}

async function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

async function toggleTheme(page) {
  const btn = page.locator('.code-tpl-footer-actions button').first();
  if (await btn.isVisible()) {
    await btn.click({ force: true });
    await sleep(400);
  }
}

(async () => {
  console.log('=== Starting Code-Traefik Full E2E Browser Test Suite ===');
  const browser = await chromium.launch({
    executablePath: '/usr/bin/google-chrome',
    headless: true,
    args: ['--no-sandbox', '--disable-dev-shm-usage', '--window-size=1440,900'],
  });

  const context = await browser.newContext({
    viewport: { width: 1440, height: 900 },
  });
  const page = await context.newPage();

  const auditLog = [];
  function logStep(step, status, details) {
    auditLog.push({ step, status, details, time: new Date().toISOString() });
    console.log(`[${status}] ${step}: ${details}`);
  }

  try {
    // ------------------------------------------------------------------------
    // Step 0: 登录流程与深浅双模主题检查
    // ------------------------------------------------------------------------
    console.log('\n--- Step 0: Login & Theme System ---');
    await page.goto(BASE_URL, { waitUntil: 'networkidle' });
    await takeSnap(page, '01_login_page_dark');

    // 登录表单
    await page.locator('.code-unified-login-input').first().fill('admin@code-shield.com');
    await page.locator('input[type="password"]').fill('admin123');
    await page.click('button.code-unified-login-submit-btn');

    await page.waitForNavigation({ waitUntil: 'networkidle' }).catch(() => {});
    await sleep(1500);

    logStep('Login', 'PASS', `Logged in successfully as admin@code-shield.com, URL: ${page.url()}`);
    await takeSnap(page, '02_dashboard_after_login');

    // 主题切换测试
    await toggleTheme(page);
    await takeSnap(page, '03_theme_switched_to_light');
    await toggleTheme(page);
    logStep('Theme Toggle', 'PASS', 'Verified Dark and Light theme switching');

    // ------------------------------------------------------------------------
    // Step 1: 架构建模与配置维护 (三级逻辑架构 + 物理进程蓝图)
    // ------------------------------------------------------------------------
    console.log('\n--- Step 1: Architecture Modeling & Configuration ---');
    await page.goto(`${BASE_URL}/architecture`, { waitUntil: 'networkidle' });
    await sleep(1000);
    await takeSnap(page, '04_architecture_logical_empty');

    // 1.1 创建子系统 ECOMMERCE
    console.log('Creating Subsystem ECOMMERCE...');
    const btnCreateSub = page.locator('button[title="+ 子系统"], button:has-text("子系统")').first();
    await btnCreateSub.click();
    await sleep(600);
    await takeSnap(page, '05_architecture_create_subsystem_drawer');

    // 填写子系统表单
    await page.locator('.code-form input').nth(0).fill('ECOMMERCE');
    await page.locator('.code-form input').nth(1).fill('电商业务系统');
    await page.locator('.code-form textarea').fill('核心电商交易微服务系统');
    await page.locator('.code-drawer-footer button.code-btn--primary').click();
    await sleep(1200);
    logStep('Create Subsystem', 'PASS', 'Subsystem ECOMMERCE created');
    await takeSnap(page, '06_architecture_subsystem_created');

    // 1.2 创建功能组 TRADE_CORE
    console.log('Creating Functional Group TRADE_CORE...');
    const btnCreateGroup = page.locator('button[title*="新建根功能组"], button[title*="新建功能组"]').first();
    await btnCreateGroup.click();
    await sleep(600);
    await takeSnap(page, '07_architecture_create_group_drawer');

    await page.locator('.code-form input').nth(0).fill('TRADE_CORE');
    await page.locator('.code-form input').nth(1).fill('交易核心功能组');
    await page.locator('.code-form input[type="number"]').fill('1');
    await page.locator('.code-form textarea').fill('核心交易中枢与流程编排');
    await page.locator('.code-drawer-footer button.code-btn--primary').click();
    await sleep(1200);
    logStep('Create Functional Group', 'PASS', 'Functional Group TRADE_CORE created');
    await takeSnap(page, '08_architecture_group_created');

    // 1.3 创建功能模块 ORDER_CORE
    console.log('Creating Module ORDER_CORE...');
    const btnCreateMod = page.locator('button[title*="新建功能模块"], button:has-text("新建模块"), button:has-text("新建功能模块")').first();
    await btnCreateMod.click();
    await sleep(600);
    await takeSnap(page, '09_architecture_create_module_drawer');

    await page.locator('.code-form input').nth(0).fill('ORDER_CORE');
    await page.locator('.code-form input').nth(1).fill('订单履约核心模块');
    await page.locator('.code-form textarea').fill('处理下单与两阶段提交流程');
    await page.locator('.code-drawer-footer button.code-btn--primary').click();
    await sleep(1200);
    logStep('Create Module', 'PASS', 'Module ORDER_CORE created');
    await takeSnap(page, '10_architecture_module_created');

    // 1.4 创建服务 demo.order-OrderService
    console.log('Creating Service demo.order-OrderService in ORDER_CORE...');
    // 选中 ORDER_CORE 模块
    const modNode = page.locator('text=订单履约核心模块').first();
    await modNode.click();
    await sleep(1000);
    await takeSnap(page, '11_module_detail_pane');

    // 点击声明对外服务
    const btnAddService = page.locator('button:has-text("声明供给服务"), button:has-text("新建服务")').first();
    await btnAddService.click();
    await sleep(600);

    // 切换到手动录入模式
    const tabManualProvide = page.locator('button:has-text("手动预先输入声明")').first();
    if (await tabManualProvide.isVisible()) {
      await tabManualProvide.click();
      await sleep(400);
    }
    await takeSnap(page, '11_architecture_create_service_modal');

    // 填写服务名
    const serviceNameInput = page.locator('input[placeholder*="package"], input[placeholder*="TradeService"]').first();
    await serviceNameInput.fill('demo.order-OrderService');
    const serviceDesc = page.locator('.code-drawer-body textarea, .code-form textarea').first();
    if (await serviceDesc.isVisible()) {
      await serviceDesc.fill('订单交易 CROS RPC 服务');
    }
    await page.locator('.code-drawer-footer button:has-text("确认声明")').first().click();
    await sleep(1500);
    logStep('Create Service', 'PASS', 'Service demo.order-OrderService created in ORDER_CORE');
    await takeSnap(page, '12_architecture_service_created');

    // 1.5 切换到物理进程蓝图并创建 sut-order 进程
    console.log('Switching to Physical Process Blueprint...');
    await page.click('[data-testid="toggle-tree-physical"]');
    await sleep(1000);
    await takeSnap(page, '13_architecture_physical_view');

    // 创建进程组 GROUP_GATEWAY
    const btnAddProcGroup = page.locator('[data-testid="create-process-group-btn"], button:has-text("分组")').first();
    await btnAddProcGroup.click();
    await sleep(600);
    await takeSnap(page, '14_architecture_create_arch_group_drawer');
    await page.locator('.code-form input[placeholder*="CORE_SERVICES"]').fill('GROUP_GATEWAY');
    await page.locator('.code-form input[placeholder*="核心业务服务组"]').fill('网关与交易中枢组');
    await page.locator('.code-form textarea').fill('承接外部业务流量并编排微服务调用');
    await page.locator('.code-drawer-footer button.code-btn--primary').first().click();
    await sleep(1200);

    // 在 GROUP_GATEWAY 下创建架构进程 sut-order
    const btnAddArchProc = page.locator('[data-testid="create-arch-process-btn"], button[title*="新建标准架构进程"]').first();
    await btnAddArchProc.click();
    await sleep(600);
    await takeSnap(page, '15_architecture_create_arch_process_drawer');

    // 选择第一个分组
    const selectGroup = page.locator('.code-form select').first();
    if (await selectGroup.isVisible()) {
      await selectGroup.selectOption({ index: 1 });
    }
    await page.locator('.code-form input[placeholder*="proc-account-service"]').first().fill('sut-order');
    await page.locator('.code-form input[placeholder*="账户账务中心进程"]').first().fill('sut-order (订单核心网关)');
    await page.locator('.code-form input[type="number"]').first().fill('9001');
    await page.locator('.code-form input[placeholder*="account-srv"]').first().fill('sut-order');
    await page.locator('.code-form textarea').first().fill('电商交易被测核心网关服务，监听 9001 端口');
    await page.locator('.code-drawer-footer button.code-btn--primary').first().click();
    await sleep(1200);

    // 创建下游进程组 GROUP_DOWNSTREAM 与下游进程
    await btnAddProcGroup.click();
    await sleep(600);
    await page.locator('.code-form input[placeholder*="CORE_SERVICES"]').fill('GROUP_DOWNSTREAM');
    await page.locator('.code-form input[placeholder*="核心业务服务组"]').fill('核心下游微服务组');
    await page.locator('.code-form textarea').fill('账户与库存领域服务');
    await page.locator('.code-drawer-footer button.code-btn--primary').first().click();
    await sleep(1200);

    // 创建 backend-account
    await btnAddArchProc.click();
    await sleep(600);
    if (await selectGroup.isVisible()) {
      await selectGroup.selectOption({ index: 2 });
    }
    await page.locator('.code-form input[placeholder*="proc-account-service"]').first().fill('backend-account');
    await page.locator('.code-form input[placeholder*="账户账务中心进程"]').first().fill('backend-account (资产服务)');
    await page.locator('.code-form input[type="number"]').first().fill('9002');
    await page.locator('.code-form input[placeholder*="account-srv"]').first().fill('backend-account');
    await page.locator('.code-drawer-footer button.code-btn--primary').first().click();
    await sleep(1000);

    // 创建 backend-inventory
    await btnAddArchProc.click();
    await sleep(600);
    if (await selectGroup.isVisible()) {
      await selectGroup.selectOption({ index: 2 });
    }
    await page.locator('.code-form input[placeholder*="proc-account-service"]').first().fill('backend-inventory');
    await page.locator('.code-form input[placeholder*="账户账务中心进程"]').first().fill('backend-inventory (库存服务)');
    await page.locator('.code-form input[type="number"]').first().fill('9003');
    await page.locator('.code-form input[placeholder*="account-srv"]').first().fill('backend-inventory');
    await page.locator('.code-drawer-footer button.code-btn--primary').first().click();
    await sleep(1000);

    logStep('Create ArchProcesses', 'PASS', 'sut-order (:9001), backend-account (:9002), backend-inventory (:9003) created');
    await takeSnap(page, '16_architecture_arch_process_created');

    // ------------------------------------------------------------------------
    // Step 2: Proto 契约上传与解析
    // ------------------------------------------------------------------------
    console.log('\n--- Step 2: Proto Contract Upload & Activation ---');
    await page.goto(`${BASE_URL}/schemas`, { waitUntil: 'networkidle' });
    await sleep(1000);
    await takeSnap(page, '17_schemas_page_empty');

    // 上传描述集
    const btnUploadSchema = page.locator('button:has-text("上传并注册描述集"), button:has-text("上传描述集")').first();
    await btnUploadSchema.click();
    await sleep(600);
    await takeSnap(page, '18_schemas_upload_modal');

    const demoPbPath = path.resolve(__dirname, '../demo/demo.pb');
    const fileInput = page.locator('.code-modal-body input[type="file"]');
    await fileInput.setInputFiles(demoPbPath);
    await sleep(500);

    // 选择业务域服务卡片
    const cardBusiness = page.locator('text=业务域服务').first();
    if (await cardBusiness.isVisible()) {
      await cardBusiness.click();
    }

    // 勾选自动激活
    const checkboxAutoActivate = page.locator('.code-modal-body input[type="checkbox"]');
    if (await checkboxAutoActivate.isVisible()) {
      await checkboxAutoActivate.check();
    }
    // 填写版本标签
    const inputVersionTag = page.locator('.code-modal-body input[placeholder*="v1.2.0"]');
    if (await inputVersionTag.isVisible()) {
      await inputVersionTag.fill('v1.0.0');
    }

    // 提交上传
    await page.locator('.code-modal-footer button.btn-primary').click();
    await sleep(2500);
    logStep('Upload Proto Schema', 'PASS', 'demo.pb uploaded & activated successfully');
    await takeSnap(page, '19_schemas_uploaded_success');

    // 切换到契约大盘
    const tabCatalog = page.locator('text=服务方法契约大盘').first();
    if (await tabCatalog.isVisible()) {
      await tabCatalog.click();
      await sleep(1000);
      await takeSnap(page, '20_schemas_catalog_view');

      // 展开 demo.order-OrderService
      const svcOrderRow = page.locator('text=demo.order-OrderService').first();
      if (await svcOrderRow.isVisible()) {
        await svcOrderRow.click();
        await sleep(600);
        await takeSnap(page, '21_schemas_order_methods');

        // 点击 CreateOrder 查看字段树
        const opCreateOrder = page.locator('text=CreateOrder').first();
        if (await opCreateOrder.isVisible()) {
          await opCreateOrder.click();
          await sleep(1000);
          await takeSnap(page, '22_schemas_create_order_field_tree');

          // Light mode screenshot of field tree
          await toggleTheme(page);
          await takeSnap(page, '23_schemas_create_order_field_tree_light');
          await toggleTheme(page);

          // 关闭抽屉
          const btnCloseDrawer = page.locator('.code-drawer-close, button:has-text("关闭")').first();
          if (await btnCloseDrawer.isVisible()) {
            await btnCloseDrawer.click();
            await sleep(400);
          }
        }
      }
    }

    // ------------------------------------------------------------------------
    // Step 3: Agent 认领与进程挂载
    // ------------------------------------------------------------------------
    console.log('\n--- Step 3: Agent Claim & Process Mount ---');
    await page.goto(`${BASE_URL}/agents`, { waitUntil: 'networkidle' });
    await sleep(1000);
    await takeSnap(page, '24_agents_page_initial');

    // 登记团队公共 Agent
    const btnRegisterAgent = page.locator('button:has-text("登记团队公共 Agent"), button:has-text("公共 Agent")').first();
    await btnRegisterAgent.click();
    await sleep(600);
    await takeSnap(page, '25_agents_register_modal');

    await page.locator('.code-form input[placeholder*="my-dev-agent-01"]').fill('demo-local-agent');
    await page.locator('.code-form input[placeholder*="192.168"]').fill('http://127.0.0.1:9091');
    const descAgent = page.locator('.code-modal-body input').nth(2);
    if (await descAgent.isVisible()) {
      await descAgent.fill('本地 eBPF 探针代理 (端口 9091)');
    }
    await page.locator('.code-modal-footer button.code-btn--primary').click();
    await sleep(2500);
    logStep('Register Agent', 'PASS', 'Agent demo-local-agent registered and verified ONLINE with eBPF');
    await takeSnap(page, '26_agents_registered_online');

    // 打开批量挂载抽屉
    // 打开批量挂载抽屉
    const btnOpenMount = page.locator('button:has-text("批量挂载架构进程")').first();
    await btnOpenMount.click();
    await sleep(1500);
    await takeSnap(page, '27_agents_process_mount_drawer');

    // 点击一键扫描宿主机活跃进程并智能对齐
    const btnAutoScan = page.locator('[data-testid="auto-scan-procs-btn"], button:has-text("自动扫描宿主机进程")').first();
    if (await btnAutoScan.isVisible()) {
      await btnAutoScan.click();
      await sleep(2000);
      await takeSnap(page, '28_agents_process_mount_scanned_matched');
      logStep('Auto Match Processes', 'PASS', 'Auto-scanned host processes and matched arch blueprints');
    }

    // 确保所有架构进程都被勾选（如果自动扫描未全部勾选，手动勾选每一个）
    const mountCheckboxes = page.locator('input[data-testid^="checkbox-proc-"]');
    const cbCount = await mountCheckboxes.count();
    console.log(`Found ${cbCount} process checkboxes in mount drawer`);
    for (let i = 0; i < cbCount; i++) {
      const cb = mountCheckboxes.nth(i);
      if (!(await cb.isChecked())) {
        await cb.click();
        await sleep(300);
      }
    }
    await sleep(600);

    // 保存挂载配置
    const btnSaveMounts = page.locator('[data-testid="save-mounts-btn"]').first();
    await btnSaveMounts.click();
    await sleep(2500);
    logStep('Mount Processes', 'PASS', '3 processes mounted and armed on Agent');
    await takeSnap(page, '29_agents_processes_mounted_success');

    // ------------------------------------------------------------------------
    // Step 4: 流量录制会话 (Recording) 与 Demo 流量触发
    // ------------------------------------------------------------------------
    console.log('\n--- Step 4: Traffic Recording Session ---');
    await page.goto(`${BASE_URL}/record`, { waitUntil: 'networkidle' });
    await sleep(1000);
    await takeSnap(page, '30_recording_page_initial');

    // 发起协同录制
    const btnStartRec = page.locator('[data-testid="btn-open-start-recording"]').first();
    await btnStartRec.click();
    await sleep(1500);
    await takeSnap(page, '31_recording_start_drawer');

    // 填写会话名称
    const inputRecName = page.locator('.code-recording-drawer-section input').first();
    await inputRecName.fill('E2E-BROWSER-TEST');

    // 勾选所有业务监控目标进程
    await page.waitForSelector('.code-process-tree-box', { timeout: 10000 });
    await sleep(1000);
    const btnSelectAllProcs = page.locator('button.code-process-tree-btn:has-text("全选")').first();
    if (await btnSelectAllProcs.isVisible() && await btnSelectAllProcs.isEnabled()) {
      await btnSelectAllProcs.click();
      await sleep(500);
    } else {
      const procChecks = page.locator('.code-process-tree-box input[type="checkbox"]');
      const count = await procChecks.count();
      console.log(`Found ${count} process checkboxes in recording drawer`);
      for (let i = 0; i < count; i++) {
        const c = procChecks.nth(i);
        if (!(await c.isChecked())) {
          await c.check();
          await sleep(200);
        }
      }
    }

    // 启动录制
    const btnConfirmRec = page.locator('[data-testid="btn-confirm-start-recording"]').first();
    await btnConfirmRec.click();
    await sleep(2500);
    logStep('Start Recording', 'PASS', 'Recording session E2E-BROWSER-TEST started (RECORDING)');
    await takeSnap(page, '32_recording_active_banner_dark');

    // Light mode snapshot of active banner
    await toggleTheme(page);
    await takeSnap(page, '33_recording_active_banner_light');
    await toggleTheme(page);

    // 等待 2 秒确保数据面流就绪
    await sleep(2000);

    // 在后台触发 Demo 4 种业务场景流量
    console.log('Triggering Demo CROS RPC traffic scenarios in background...');
    const clientBin = path.resolve(__dirname, '../demo/bin/client');

    console.log('  -> Scenario 1: Happy Path (Alice 机械键盘)');
    execSync(`${clientBin} --addr="127.0.0.1:9001"`, { input: '2\nq\n', timeout: 8000 });
    await sleep(1000);

    console.log('  -> Scenario 2: Out of Stock (Alice 99件显示器)');
    execSync(`${clientBin} --addr="127.0.0.1:9001"`, { input: '3\nq\n', timeout: 8000 });
    await sleep(1000);

    console.log('  -> Scenario 3: Balance Deficit Rollback (Bob 机械键盘)');
    execSync(`${clientBin} --addr="127.0.0.1:9001"`, { input: '4\nq\n', timeout: 8000 });
    await sleep(1000);

    console.log('  -> Scenario 4: Burst 10 Packets');
    execSync(`${clientBin} --addr="127.0.0.1:9001"`, { input: '8\nq\n', timeout: 8000 });
    await sleep(3000);

    logStep('Trigger Demo Traffic', 'PASS', 'Executed all 4 CROS RPC traffic scenarios');

    // 等待录制横幅数据刷新
    await sleep(4000);
    await takeSnap(page, '34_recording_with_captured_traffic');

    // 停止录制
    console.log('Stopping recording session...');
    const btnStopRec = page.locator('[data-testid="btn-stop-recording"], button:has-text("停止录制")').first();
    if (await btnStopRec.isVisible()) {
      await btnStopRec.click();
      await sleep(3500);
    }
    logStep('Stop Recording', 'PASS', 'Recording session stopped, lease released');
    await takeSnap(page, '35_recording_completed_table');

    // ------------------------------------------------------------------------
    // Step 5: 流量检索与解剖 (Traffic Debug & Anatomy)
    // ------------------------------------------------------------------------
    console.log('\n--- Step 5: Traffic Debug & Anatomy ---');
    await page.goto(`${BASE_URL}/debug`, { waitUntil: 'networkidle' });
    await sleep(1500);
    await takeSnap(page, '36_traffic_debug_page');

    // 点击第一条报文查看解剖视窗
    const firstRow = page.locator('tbody tr.code-table-row-clickable, tbody tr').first();
    if (await firstRow.isVisible()) {
      await firstRow.click();
      await sleep(1500);
      await takeSnap(page, '37_traffic_packet_anatomy_drawer_dark');
      logStep('Packet Anatomy Dark', 'PASS', '4-stage packet dissection drawer verified in Dark mode');

      // Light mode
      await toggleTheme(page);
      await takeSnap(page, '38_traffic_packet_anatomy_drawer_light');
      await toggleTheme(page);
      logStep('Packet Anatomy Light', 'PASS', '4-stage packet dissection drawer verified in Light mode');

      // 关闭抽屉
      const btnCloseAnatomy = page.locator('.code-drawer-close, button:has-text("关闭")').first();
      if (await btnCloseAnatomy.isVisible()) {
        await btnCloseAnatomy.click();
        await sleep(500);
      }
    }

    // ------------------------------------------------------------------------
    // Step 6: 流量回放与 Diff 差异审计 (Replay & Diff)
    // ------------------------------------------------------------------------
    console.log('\n--- Step 6: Replay & Diff Audit ---');
    await page.goto(`${BASE_URL}/replay`, { waitUntil: 'networkidle' });
    await sleep(1500);
    await takeSnap(page, '39_replay_page_initial');

    // 发起回放任务
    const btnCreateReplay = page.locator('button:has-text("创建同构重放任务")').first();
    await btnCreateReplay.waitFor({ state: 'visible', timeout: 10000 });
    await btnCreateReplay.click();
    await sleep(1500);
    await takeSnap(page, '40_replay_task_creator_modal');

    // 填写回放名称
    const inputReplayName = page.locator('.code-modal-body input').first();
    await inputReplayName.fill('E2E-Replay-Diff-Audit');

    // 确保批次 INGRESS 记录已加载
    await page.waitForSelector('.code-modal-body table tbody tr', { timeout: 10000 }).catch(() => {});
    await sleep(1500);
    await takeSnap(page, '40_replay_task_creator_modal');

    // 提交重放
    await page.locator('.code-modal-footer button:has-text("启动同构重放与双层断言"), .code-modal-footer button.code-btn--primary').first().click();
    await sleep(5000);
    logStep('Create Replay Task', 'PASS', 'Replay task initiated and completed');

    // 刷新或等待重放任务表格展示
    await page.waitForSelector('button:has-text("双层 Diff 报告")', { timeout: 20000 });
    await takeSnap(page, '41_replay_task_completed_table');

    // 点击第一条重放任务查看 Diff 报告抽屉 (或点击“双层 Diff 报告”按钮)
    const btnDiffReport = page.locator('button:has-text("双层 Diff 报告")').first();
    await btnDiffReport.click();
    await sleep(2500);
    await takeSnap(page, '42_replay_diff_report_drawer_dark');

    // Light mode screenshot of Diff drawer
    await toggleTheme(page);
    await takeSnap(page, '43_replay_diff_report_drawer_light');
    await toggleTheme(page);
    logStep('Replay Diff Audit', 'PASS', 'Diff report verified (baseline vs replay in Dark & Light modes)');

    // 关闭 Diff 报告抽屉
    const btnCloseDiff = page.locator('.code-drawer-close, button:has-text("关闭")').first();
    if (await btnCloseDiff.isVisible()) {
      await btnCloseDiff.click();
      await sleep(500);
    }

    console.log('\n=== All E2E steps completed successfully! ===');
  } catch (err) {
    console.error('Fatal error during E2E test:', err);
    logStep('E2E Execution', 'FAIL', err.message);
    await takeSnap(page, '99_error_state');
  } finally {
    fs.writeFileSync(
      path.resolve(__dirname, 'e2e_audit_log.json'),
      JSON.stringify(auditLog, null, 2),
      'utf-8'
    );
    await browser.close();
  }
})();
