<template>
  <router-link to="/lottery" class="lottery-entry" :aria-label="t('lottery.title')" :title="t('lottery.schedule')">
    <Icon name="gift" size="sm" />
    <span>{{ t('lottery.entry') }}</span>
  </router-link>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
const { t } = useI18n()
</script>

<style scoped>
.lottery-entry {
  position: relative;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: .375rem;
  min-height: 2.5rem;
  flex-shrink: 0;
  padding: .5rem .75rem;
  border-radius: .75rem;
  background: #c72d40;
  color: white;
  font-size: .875rem;
  font-weight: 650;
  isolation: isolate;
  transition: background .2s, transform .2s;
}
.lottery-entry::before {
  content: '';
  position: absolute;
  inset: -.2rem;
  border-radius: inherit;
  z-index: -1;
  background: #dc354b;
  animation: lottery-breathe 2.8s ease-in-out infinite;
}
.lottery-entry:hover { background: #ae2435; }
.lottery-entry:active { transform: scale(.98); }
.lottery-entry:focus-visible { outline: 3px solid #fb7185; outline-offset: 4px; }
@keyframes lottery-breathe {
  0%, 100% { opacity: .12; transform: scale(.98); }
  50% { opacity: .35; transform: scale(1.07); }
}
@media (prefers-reduced-motion: reduce) {
  .lottery-entry::before { animation: none; opacity: .15; }
  .lottery-entry { transition: none; }
}
</style>
