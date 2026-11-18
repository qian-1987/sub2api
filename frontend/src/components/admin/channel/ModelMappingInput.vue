<template>
  <span class="relative block min-w-0 flex-1">
    <input
      v-bind="$attrs"
      :value="model"
      :title="model || undefined"
      type="text"
      class="input peer text-xs"
    />
    <span
      v-if="displayName !== model"
      aria-hidden="true"
      class="input pointer-events-none absolute inset-0 truncate text-xs peer-focus:hidden"
      :class="$attrs.class"
    >{{ displayName }}</span>
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { channelModelDisplayName } from './modelDisplayName'

defineOptions({ inheritAttrs: false })

const props = defineProps<{ model: string; platform: string }>()
const displayName = computed(() => channelModelDisplayName(props.model, props.platform))
</script>
