<script setup>
import { ref, watch, onMounted } from 'vue'
import { api, today, thisMonth } from '../api.js'

const period = ref(today().slice(0, 7))
const settlements = ref([])
const settlement = ref(null)
const meals = ref([])
const adjustments = ref([])
const reconcile = ref(null)
const elders = ref([])
const adjElder = ref('')
const adjReason = ref('')
const preview = ref(null)
const msg = ref(null)

function flash(kind, text, timeout = 6000) {
  msg.value = { kind, text }
  if (timeout) setTimeout(() => (msg.value = null), timeout)
}

async function loadAll() {
  await Promise.all([loadSettlements(), loadElders()])
  await loadPeriod()
}

async function loadSettlements() {
  settlements.value = await api.settlements()
}

async function loadElders() {
  elders.value = await api.listElders('')
  if (!adjElder.value && elders.value.length) adjElder.value = elders.value[0].id
}

async function loadPeriod() {
  settlement.value = settlements.value.find((s) => s.period === period.value) || null
  reconcile.value = null
  preview.value = null
  if (settlement.value) {
    const [m, a, r] = await Promise.all([
      api.settlementMeals(period.value),
      api.adjustments(period.value),
      api.reconcile(period.value),
    ])
    meals.value = m
    adjustments.value = a
    reconcile.value = r
  } else {
    meals.value = []
    adjustments.value = []
  }
}

async function doSettle() {
  try {
    const r = await api.createSettlement(period.value)
    flash('ok', `${period.value} 月结单已生成/刷新，覆盖 ${r.settlement.total_meals} 笔未结算餐次`)
    await loadSettlements()
    await loadPeriod()
  } catch (e) {
    flash('err', e.message)
  }
}

async function doRecalc() {
  try {
    const r = await api.recalc(Number(adjElder.value) || 0)
    flash('ok', r.message)
    await loadPeriod()
  } catch (e) {
    flash('err', e.message)
  }
}

async function doClose() {
  if (!confirm(`确认封账 ${period.value}？封账后餐次补贴快照冻结，发现错误只能出具补差单。`)) return
  try {
    const r = await api.closeSettlement(period.value)
    flash('ok', r.message)
    await loadSettlements()
    await loadPeriod()
  } catch (e) {
    flash('err', e.message)
  }
}

async function doPreviewAdj() {
  try {
    preview.value = await api.previewAdjustment(period.value, Number(adjElder.value))
  } catch (e) {
    preview.value = null
    flash('err', e.message)
  }
}

async function doCreateAdj() {
  try {
    const r = await api.createAdjustment(period.value, Number(adjElder.value), adjReason.value)
    flash('ok', r.message + '；补差单 #' + r.adjustment.id)
    adjReason.value = ''
    preview.value = null
    await loadPeriod()
  } catch (e) {
    flash('err', e.message)
  }
}

const elderName = (id) => elders.value.find((e) => e.id === id)?.name || ('#' + id)
const mealCN = (t) => (t === 'dinner' ? '晚餐' : '午餐')
const parseDates = (s) => { try { return JSON.parse(s).join('、') } catch { return s } }

watch(period, loadPeriod)
onMounted(loadAll)
</script>

<template>
  <div class="rule-box">
    <b>月结流程：</b>① 发现资格登记有误 → 先在社区端修正 → <b>重新计算</b>尚未结算的餐次；
    ② 生成月结单核对汇总；③ <b>封账</b>后补贴快照冻结。封账后再发现错误，
    <b>原结算结果保留不动</b>，只能<b>追加补差单</b>（列清受影响日期、新旧补贴、差额及模拟补差流水）。
  </div>

  <div v-if="msg" :class="['msg', msg.kind]">{{ msg.text }}</div>

  <div class="card">
    <div class="row">
      <div class="field">
        <label>结算月份</label>
        <input type="month" v-model="period" />
      </div>
      <button class="btn secondary" @click="doRecalc">重新计算未结算餐次（按所选老人）</button>
      <button class="btn" @click="doSettle">生成 / 刷新月结单</button>
      <button class="btn warn" :disabled="!settlement || settlement.status === 'closed'" @click="doClose">封账</button>
      <div class="field">
        <label>用于重算/补差的老人</label>
        <select v-model="adjElder" style="min-width:180px">
          <option v-for="e in elders" :key="e.id" :value="e.id">{{ e.name }}</option>
        </select>
      </div>
    </div>
  </div>

  <!-- 结算单概览 -->
  <div class="card" v-if="settlement">
    <h2>
      {{ period }} 结算单 #{{ settlement.id }}
      <span :class="['badge', settlement.status]" style="margin-left:8px">
        {{ settlement.status === 'closed' ? '已封账' : '月结中（可重算）' }}
      </span>
      <span v-if="settlement.closed_at" class="muted" style="margin-left:10px;font-weight:400">
        封账时间 {{ settlement.closed_at.replace('T', ' ').slice(0, 16) }}
      </span>
    </h2>
    <div class="stat-row">
      <div class="stat"><div class="k">餐次笔数</div><div class="v">{{ settlement.total_meals }}</div></div>
      <div class="stat"><div class="k">餐费合计</div><div class="v">¥{{ Number(settlement.total_price).toFixed(2) }}</div></div>
      <div class="stat"><div class="k">补贴合计（冻结口径）</div><div class="v" style="color:#03543f">¥{{ Number(settlement.total_subsidy).toFixed(2) }}</div></div>
      <div class="stat"><div class="k">自付合计</div><div class="v">¥{{ Number(settlement.total_self_pay).toFixed(2) }}</div></div>
    </div>
  </div>
  <div class="card muted" v-else>该月尚无结算单，点击「生成 / 刷新月结单」开始月结。</div>

  <!-- 对账 -->
  <div class="card" v-if="reconcile">
    <h2>对账核对</h2>
    <div class="stat-row">
      <div class="stat">
        <div class="k">补贴流水合计（逐餐相加）</div>
        <div class="v">¥{{ Number(reconcile.subsidy_from_meals).toFixed(2) }}</div>
      </div>
      <div class="stat">
        <div class="k">结算单补贴金额</div>
        <div class="v">¥{{ Number(reconcile.settlement_total_subsidy).toFixed(2) }}</div>
      </div>
      <div class="stat">
        <div class="k">补差单净额（追加）</div>
        <div class="v" :style="{ color: reconcile.adjustment_diff >= 0 ? '#03543f' : '#9b1c1c' }">
          {{ reconcile.adjustment_diff >= 0 ? '+' : '' }}¥{{ Number(reconcile.adjustment_diff).toFixed(2) }}
        </div>
      </div>
      <div class="stat">
        <div class="k">最终应付补贴 = 原结算 + 补差</div>
        <div class="v" style="color:#134e6f">¥{{ Number(reconcile.final_subsidy_payable).toFixed(2) }}</div>
      </div>
    </div>
    <div :class="['msg', reconcile.balanced ? 'ok' : 'err']" style="margin-top:12px">
      <b>{{ reconcile.balanced ? '✅ 对账平衡' : '❌ 对账不平' }}</b>
      <span style="margin-left:8px">
        流水 {{ reconcile.meals_in_settlement }} 笔与结算单 {{ settlement.total_meals }} 笔逐笔核对；
        补差单 {{ reconcile.adjustment_count }} 张。
      </span>
    </div>
    <ul v-if="reconcile.mismatches?.length">
      <li v-for="(m2, i) in reconcile.mismatches" :key="i" class="msg err">{{ m2 }}</li>
    </ul>
  </div>

  <!-- 封账后补差 -->
  <div class="card" v-if="settlement && settlement.status === 'closed'">
    <h2>封账后纠错：补差单（只追加，不改原结算）</h2>
    <div class="row">
      <div class="field">
        <label>受影响老人</label>
        <select v-model="adjElder" style="min-width:180px">
          <option v-for="e in elders" :key="e.id" :value="e.id">{{ e.name }}</option>
        </select>
      </div>
      <button class="btn secondary" @click="doPreviewAdj">按当前资格预览差异</button>
    </div>

    <div v-if="preview" style="margin-top:12px">
      <p class="muted">{{ preview.message }}</p>
      <table v-if="preview.affected?.length">
        <thead>
          <tr>
            <th>就餐日期</th><th>餐次</th>
            <th class="num">餐费</th>
            <th class="num">原补贴（封账快照）</th>
            <th class="num">重算补贴</th>
            <th class="num">差额</th>
            <th>现依据</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(a, i) in preview.affected" :key="i">
            <td>{{ a.Meal.meal_date }}</td>
            <td>{{ mealCN(a.Meal.meal_type) }}</td>
            <td class="num money">{{ Number(a.Meal.price).toFixed(2) }}</td>
            <td class="num money">¥{{ Number(a.Meal.subsidy_amount).toFixed(2) }}</td>
            <td class="num money">¥{{ Number(a.NewSubsidy).toFixed(2) }}</td>
            <td class="num money" :style="{ color: a.Diff >= 0 ? '#03543f' : '#9b1c1c', fontWeight: 700 }">
              {{ a.Diff >= 0 ? '+' : '' }}¥{{ Number(a.Diff).toFixed(2) }}
            </td>
            <td class="basis" style="max-width:280px">{{ elderName(a.Meal.elderly_id) }} 当日{{ a.NewStatus === 'active' ? '生效' : '暂停' }}</td>
          </tr>
        </tbody>
      </table>
      <div class="row" v-if="preview.affected?.length" style="margin-top:12px">
        <div class="field" style="flex:1">
          <label>补差原因（写入补差单）</label>
          <input v-model="adjReason" placeholder="如：补贴标准误登记为10元，应为12元" style="min-width:300px;width:100%" />
        </div>
        <button class="btn warn" @click="doCreateAdj">出具补差单</button>
      </div>
      <p v-if="!preview.affected?.length" class="msg info">该老人本月无差异餐次，无需出具补差单。</p>
    </div>

    <h3 v-if="adjustments.length">已出具补差单（追加记录）</h3>
    <table v-if="adjustments.length">
      <thead>
        <tr>
          <th>#</th><th>老人</th><th>受影响日期</th><th class="num">餐次</th>
          <th class="num">原补贴</th><th class="num">重算补贴</th><th class="num">差额</th>
          <th>原因</th><th>补差支付流水</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="a in adjustments" :key="a.id">
          <td>{{ a.id }}</td>
          <td>{{ a.elderly_name }}</td>
          <td>{{ parseDates(a.affected_dates) }}</td>
          <td class="num">{{ a.meal_count }}</td>
          <td class="num money">¥{{ Number(a.old_subsidy).toFixed(2) }}</td>
          <td class="num money">¥{{ Number(a.new_subsidy).toFixed(2) }}</td>
          <td class="num">
            <span :class="['badge', a.diff_amount >= 0 ? 'plus' : 'minus']">
              {{ a.diff_amount >= 0 ? '补发' : '追回' }}
              {{ a.diff_amount >= 0 ? '+' : '' }}¥{{ Number(a.diff_amount).toFixed(2) }}
            </span>
          </td>
          <td class="basis" style="max-width:200px">{{ a.reason }}</td>
          <td class="txn">{{ a.pay_txn_id }}</td>
        </tr>
      </tbody>
    </table>
  </div>

  <!-- 结算餐次明细 -->
  <div class="card" v-if="settlement">
    <h2>{{ settlement.status === 'closed' ? '已封账餐次（快照冻结）' : '本次月结覆盖的餐次' }}</h2>
    <table>
      <thead>
        <tr>
          <th>就餐日期</th><th>餐次</th><th>老人</th>
          <th class="num">餐费</th><th class="num">补贴</th><th class="num">自付</th>
          <th>当日资格</th><th>计算依据</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="m in meals" :key="m.id">
          <td>{{ m.meal_date }}</td>
          <td>{{ mealCN(m.meal_type) }}</td>
          <td>{{ elderName(m.elderly_id) }}</td>
          <td class="num money">{{ Number(m.price).toFixed(2) }}</td>
          <td class="num money" style="color:#03543f;font-weight:600">{{ Number(m.subsidy_amount).toFixed(2) }}</td>
          <td class="num money">{{ Number(m.self_pay).toFixed(2) }}</td>
          <td><span :class="['badge', m.status === 'active' ? 'active' : 'suspended']">{{ m.status === 'active' ? '生效' : '暂停' }}</span></td>
          <td class="basis" style="max-width:330px">{{ m.basis_detail }}</td>
        </tr>
        <tr v-if="!meals.length"><td colspan="8" class="muted">本月暂无餐次</td></tr>
      </tbody>
    </table>
  </div>

  <!-- 历史月份 -->
  <div class="card">
    <h2>全部月份结算单</h2>
    <table>
      <thead>
        <tr><th>月份</th><th>状态</th><th class="num">餐次</th><th class="num">餐费</th><th class="num">补贴</th><th class="num">自付</th><th>封账时间</th></tr>
      </thead>
      <tbody>
        <tr v-for="s in settlements" :key="s.id" style="cursor:pointer" @click="period = s.period">
          <td><b>{{ s.period }}</b></td>
          <td><span :class="['badge', s.status]">{{ s.status === 'closed' ? '已封账' : '月结中' }}</span></td>
          <td class="num">{{ s.total_meals }}</td>
          <td class="num money">{{ Number(s.total_price).toFixed(2) }}</td>
          <td class="num money">{{ Number(s.total_subsidy).toFixed(2) }}</td>
          <td class="num money">{{ Number(s.total_self_pay).toFixed(2) }}</td>
          <td class="muted">{{ s.closed_at ? s.closed_at.replace('T', ' ').slice(0, 16) : '—' }}</td>
        </tr>
        <tr v-if="!settlements.length"><td colspan="7" class="muted">暂无结算单</td></tr>
      </tbody>
    </table>
  </div>
</template>
