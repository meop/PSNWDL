<script lang="ts">
  interface Props {
    message: string
  }

  let { message }: Props = $props()
</script>

<div class="empty">{message}</div>

<style>
  .empty {
    color: var(--c-muted-soft);
    font-size: 0.875rem;
    padding: 0.75rem;
  }
</style>
