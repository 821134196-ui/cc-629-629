<script setup>
import { ref } from 'vue'
import CommunityView from './components/CommunityView.vue'
import CanteenView from './components/CanteenView.vue'
import ElderView from './components/ElderView.vue'
import SettlementView from './components/SettlementView.vue'

const tab = ref('community')
const tabs = [
  { key: 'community', label: '社区端 · 资格登记' },
  { key: 'canteen', label: '食堂端 · 扫码核销' },
  { key: 'elder', label: '老人查询 · 每餐补贴' },
  { key: 'settlement', label: '结算 · 月结/封账/补差' },
]
</script>

<template>
  <header class="top">
    <h1>社区老人助餐资格变更与餐补结算系统</h1>
    <div class="sub">
      补贴按<b>实际就餐当天</b>的资格计算并在核销时快照 · 同一餐次只核销一次 ·
      月结前可重算 · 封账后只追加补差单
    </div>
  </header>

  <nav class="tabs">
    <button
      v-for="t in tabs" :key="t.key"
      :class="{ active: tab === t.key }"
      @click="tab = t.key"
    >{{ t.label }}</button>
  </nav>

  <CommunityView v-if="tab === 'community'" />
  <CanteenView v-else-if="tab === 'canteen'" />
  <ElderView v-else-if="tab === 'elder'" />
  <SettlementView v-else-if="tab === 'settlement'" />
</template>
