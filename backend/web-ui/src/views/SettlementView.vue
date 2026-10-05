<script setup>
import { ref, watch, onMounted } from 'vue'
import { api, fen, statusText } from '../api.js'

const props = defineProps({ seniors: Array, seniorId: Number, refreshKey: Number })
const emit = defineEmits(['update:senior-id', 'changed'])

const period = ref(new Date().toISOString().slice(0, 7))
const range = ref({ start_date: '', end_date: '' })
const adjReason = ref('资格记录登记错误，按更正后资格补差')

const settlements = ref([])
const detail = ref(null)
const recalcReport = ref(null)
const vouchers = ref([])
const reconcile = ref(null)
const msg = ref(''); const err = ref('')
const busy = ref(false)

function clearFeedback() { msg.value = ''; err.value = '' }

async function loadAll() {
  if (!props.seniorId) return
  try {
    settlements.value = await api.listSettlements(props.seniorId, period.value)
    vouchers.value = await api.listAdjustments(props.seniorId, period.value)
    reconcile.value = await api.reconcile(props.seniorId, period.value)
    detail.value = null
    if (settlements.value.length) {
      detail.value = await api.getSettlement(settlements.value[0].id)
    }
  } catch (e) { err.value = e.message }
}
onMounted(loadAll)
watch(() => [props.seniorId, period.value, props.refreshKey], loadAll)

async function doRecalc() {
  clearFeedback(); busy.value = true
  try {
    recalcReport.value = await api.recalc(props.seniorId, range.value)
    msg.value = `重算完成：检查 ${recalcReport.value.checked} 餐，变更 ${recalcReport.value.changed} 餐，跳过已封账 ${recalcReport.value.skipped_settled} 餐`
    emit('changed'); await loadAll()
  } catch (e) { err.value = e.message } finally { busy.value = false }
}

async function doClose() {
  clearFeedback()
  if (!confirm(`确认对 ${period.value} 封账？封账后金额固化，发现资格错误只能追加补差单。`)) return
  busy.value = true
  try {
    const st = await api.closeMonth(props.seniorId, period.value)
    msg.value = `封账成功：结算单 #${st.id}，补贴 ${fen(st.total_subsidy_fen)}，${st.meal_count} 餐`
    emit('changed'); await loadAll()
  } catch (e) { err.value = e.message } finally { busy.value = false }
}

async function doAdjustment() {
  clearFeedback()
  busy.value = true
  try {
    const v = await api.createAdjustment(props.seniorId, { period: period.value, reason: adjReason.value })
    msg.value = `补差单 #${v.id} 已出具：${v.meal_count} 餐，净差异 ${fen(v.total_delta_fen)}（负数表示追回/冲减）`
    emit('changed'); await loadAll()
  } catch (e) { err.value = e.message } finally { busy.value = false }
}

const settled = () => settlements.value.length > 0
</script>

<template>
  <div class="panel">
    <div class="senior-bar">
      <h2 style="margin:0">月结结算</h2>
      <select :value="seniorId" @change="emit('update:senior-id', Number($event.target.value))">
        <option v-for="s in seniors" :key="s.id" :value="s.id">{{ s.name }}（{{ s.card_no }}）</option>
      </select>
      <input type="month" v-model="period" style="max-width:170px" />
      <span class="chip" :class="settled() ? 'settled' : 'unsettled'">
        {{ settled() ? '本月已封账（结果不可变）' : '本月未封账（可重算）' }}
      </span>
    </div>

    <div v-if="msg" class="banner ok">{{ msg }}</div>
    <div v-if="err" class="banner err">{{ err }}</div>

    <!-- 封账前：重算 -->
    <div v-if="!settled()">
      <div class="sub">
        月结前发现资格登记有误：填写区间（可只填一端）后按最新登记重算「尚未封账」餐次，
        旧补贴流水作废留痕、追加新版本流水；已封账餐次自动跳过。
      </div>
      <div class="grid cols-4">
        <div><label>重算起始日（含）</label><input type="date" v-model="range.start_date" /></div>
        <div><label>重算截止日（含）</label><input type="date" v-model="range.end_date" /></div>
        <div class="row-actions"><button class="btn ghost" :disabled="busy" @click="doRecalc">封账前重算</button></div>
      </div>
    </div>

    <!-- 封账 -->
    <div class="row-actions" style="margin-top:14px">
      <button class="btn" :disabled="busy || settled()" @click="doClose">对 {{ period }} 封账</button>
      <span v-if="settled()" class="muted small">已封账，不能重复封账，也不能再改写这些餐次。</span>
    </div>

    <!-- 重算明细 -->
    <div v-if="recalcReport && recalcReport.changes">
      <table style="margin-top:14px">
        <thead><tr><th>餐次</th><th>原补贴</th><th>新补贴</th><th>差额</th><th>新计算依据</th></tr></thead>
        <tbody>
          <tr v-for="c in recalcReport.changes" :key="c.meal_id">
            <td>{{ c.meal_date }} {{ c.meal_type }}</td>
            <td class="money">{{ fen(c.old_subsidy_fen) }}</td>
            <td class="money">{{ fen(c.new_subsidy_fen) }}</td>
            <td class="money" :class="c.delta_fen >= 0 ? 'pos' : 'neg'">{{ c.delta_fen >= 0 ? '+' : '' }}{{ fen(c.delta_fen) }}</td>
            <td class="basis">{{ c.new_basis }}</td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>

  <!-- 结算单明细 -->
  <div class="panel" v-if="detail">
    <h2>结算单 #{{ detail.settlement.id }} 明细（{{ detail.settlement.period }}）</h2>
    <div class="sub">封账区间 {{ detail.settlement.start_date }} ~ {{ detail.settlement.end_date }}，金额与逐餐依据在封账时固化。</div>
    <div class="grid cols-4" style="margin-bottom:10px">
      <div class="stat"><div class="k">餐次数</div><div class="v">{{ detail.settlement.meal_count }}</div></div>
      <div class="stat"><div class="k">餐费合计</div><div class="v">{{ fen(detail.settlement.total_price_fen) }}</div></div>
      <div class="stat"><div class="k">补贴合计（原结算）</div><div class="v">{{ fen(detail.settlement.total_subsidy_fen) }}</div></div>
      <div class="stat"><div class="k">个人自付</div><div class="v">{{ fen(detail.settlement.total_self_pay_fen) }}</div></div>
    </div>
    <table>
      <thead><tr><th>就餐日期</th><th>餐次</th><th>当日资格</th><th>补贴</th><th>自付</th><th>计算依据</th></tr></thead>
      <tbody>
        <tr v-for="m in detail.meals" :key="m.id">
          <td>{{ m.meal_date }}</td><td>{{ m.meal_type }}</td>
          <td><span class="chip" :class="m.eli_status">{{ statusText[m.eli_status] }}</span></td>
          <td class="money pos">{{ fen(m.subsidy_fen) }}</td>
          <td class="money">{{ fen(m.self_pay_fen) }}</td>
          <td class="basis">{{ m.basis_describe }}</td>
        </tr>
      </tbody>
    </table>
  </div>

  <!-- 封账后补差 -->
  <div class="panel" v-if="settled()">
    <h2>封账后更正：只追加补差单</h2>
    <div class="sub">
      先在「老人与资格登记」页补录/更正资格，再出具补差单。系统按现行资格逐餐重算差异，
      但<b>原结算单与原餐次快照保持不变</b>，仅追加补差单与有符号调整流水。
    </div>
    <div class="grid cols-2">
      <div><label>补差原因</label><input v-model="adjReason" /></div>
      <div class="row-actions"><button class="btn warn" :disabled="busy" @click="doAdjustment">出具补差单</button></div>
    </div>

    <div v-for="v in vouchers" :key="v.id" class="panel" style="background:#fffdf7;border-color:#fde68a;margin-top:12px">
      <h2>补差单 #{{ v.id }} · {{ fen(v.total_delta_fen) }}
        <span class="small" :class="v.total_delta_fen >= 0 ? 'pos' : 'neg'">
          （{{ v.total_delta_fen >= 0 ? '补拨' : '追回/冲减' }}）
        </span>
      </h2>
      <div class="basis">原因：{{ v.reason }} ｜ 受影响日期：{{ v.affected_dates }} ｜ 模拟拨付流水：<code class="tx">{{ v.pay_tx_id }}</code></div>
      <table>
        <thead><tr><th>日期</th><th>餐次</th><th>原补贴</th><th>应为</th><th>差额</th><th>原依据 → 新依据</th></tr></thead>
        <tbody>
          <tr v-for="it in v.items" :key="it.id">
            <td>{{ it.meal_date }}</td><td>{{ it.meal_type }}</td>
            <td class="money">{{ fen(it.old_subsidy_fen) }}</td>
            <td class="money">{{ fen(it.new_subsidy_fen) }}</td>
            <td class="money" :class="it.delta_fen >= 0 ? 'pos' : 'neg'">{{ it.delta_fen >= 0 ? '+' : '' }}{{ fen(it.delta_fen) }}</td>
            <td class="basis">{{ it.old_basis }}<br /><b>→</b> {{ it.new_basis }}</td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>

  <!-- 对账 -->
  <div class="panel" v-if="reconcile">
    <h2>对账勾稽（{{ reconcile.period }}）</h2>
    <div class="grid cols-4" style="margin:10px 0">
      <div class="stat"><div class="k">餐次表补贴合计</div><div class="v">{{ fen(reconcile.meal_table_subsidy_fen) }}</div></div>
      <div class="stat"><div class="k">有效补贴流水</div><div class="v">{{ fen(reconcile.live_ledger_subsidy_fen) }}</div></div>
      <div class="stat"><div class="k">封账补贴 / 调整流水</div>
        <div class="v" style="font-size:15px">{{ fen(reconcile.settlement_subsidy_fen) }} / {{ fen(reconcile.adjustment_subsidy_fen) }}</div></div>
      <div class="stat"><div class="k">本月净应付补贴</div><div class="v">{{ fen(reconcile.net_payable_fen) }}</div></div>
    </div>
    <div v-for="(c, i) in reconcile.checks" :key="i" class="check-row">
      <span class="dot" :class="c.ok ? 'ok' : 'bad'"></span>
      <div><b>{{ c.name }}</b><div class="basis">{{ c.detail }}</div></div>
    </div>
  </div>
</template>
