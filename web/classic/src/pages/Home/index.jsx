/*
Copyright (C) 2025 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

import React, { useContext, useEffect, useState } from 'react';
import {
  Button,
  Typography,
  Input,
  ScrollList,
  ScrollItem,
} from '@douyinfe/semi-ui';
import { API, showError, copy, showSuccess } from '../../helpers';
import { useIsMobile } from '../../hooks/common/useIsMobile';
import { API_ENDPOINTS } from '../../constants/common.constant';
import { StatusContext } from '../../context/Status';
import { useActualTheme } from '../../context/Theme';
import { marked } from 'marked';
import { useTranslation } from 'react-i18next';
import {
  IconGithubLogo,
  IconPlay,
  IconFile,
  IconCopy,
} from '@douyinfe/semi-icons';
import { Link } from 'react-router-dom';
import NoticeModal from '../../components/layout/NoticeModal';
import {
  OpenAI,
  Claude,
  Gemini,
  Meta,
  Mistral,
  Grok,
  Aws,
  AzureAI,
  Nvidia,
  Cohere,
} from '@lobehub/icons';

const { Text } = Typography;

/* ─────────────────────────────────────────────
   天体系统样式（纯 CSS 轨道公转，无 WebGL 依赖）
   ───────────────────────────────────────────── */
const SOLAR_CSS = `
.solar-banner { position: relative; }
.solar-viewport {
  position: relative;
  width: min(86vw, 620px);
  --belt: min(86vw, 620px);
  aspect-ratio: 1 / 1;
  margin: 0 auto;
  -webkit-user-select: none;
  user-select: none;
}
.orbit-belt {
  position: absolute;
  inset: 0;
  transform: scaleY(0.62);
  transform-origin: center;
}
.orbit-ring {
  position: absolute;
  left: 50%;
  top: 50%;
  border: 1px solid rgba(143, 179, 255, 0.28);
  border-radius: 50%;
  transform: translate(-50%, -50%);
  box-shadow: 0 0 24px rgba(120, 170, 255, 0.08) inset;
}
.orbit-planet {
  position: absolute;
  left: 50%;
  top: 50%;
  width: 0;
  height: 0;
  animation: solar-orbit var(--dur) linear infinite;
  animation-delay: var(--delay);
}
.p-arranger {
  transform: translate(-50%, -50%);
}
.p-badge {
  display: grid;
  place-items: center;
  width: 30px;
  height: 30px;
  border-radius: 999px;
  background: rgba(255, 255, 255, 0.94);
  box-shadow: 0 0 18px rgba(140, 185, 255, 0.5), 0 2px 8px rgba(4, 10, 28, 0.55);
  animation: solar-spin var(--dur) linear infinite reverse;
  animation-delay: var(--delay);
}
.p-badge svg { width: 20px; height: 20px; }
@media (min-width: 768px) {
  .p-badge { width: 38px; height: 38px; }
  .p-badge svg { width: 26px; height: 26px; }
}
.solar-star {
  position: absolute;
  left: 50%;
  top: 50%;
  transform: translate(-50%, -50%);
  z-index: 3;
  pointer-events: none;
  text-align: center;
}
.solar-star .glow {
  position: absolute;
  left: 50%;
  top: 50%;
  transform: translate(-50%, -50%);
  width: clamp(160px, 44vw, 320px);
  height: clamp(160px, 44vw, 320px);
  border-radius: 50%;
  background: radial-gradient(circle,
    rgba(120, 170, 255, 0.5) 0%,
    rgba(100, 190, 220, 0.22) 45%,
    transparent 72%);
  filter: blur(2px);
}
.solar-star h1 {
  position: relative;
  font-size: clamp(1.8rem, 6.5vw, 3.2rem);
  font-weight: 800;
  letter-spacing: -0.02em;
  color: rgba(255, 255, 255, 0.95);
  text-shadow: 0 0 26px rgba(130, 180, 255, 0.85), 0 0 60px rgba(90, 200, 235, 0.5);
}
.solar-caption { position: relative; z-index: 2; }
@keyframes solar-orbit {
  from { transform: rotate(0deg) translateX(var(--r)); }
  to   { transform: rotate(360deg) translateX(var(--r)); }
}
@keyframes solar-spin {
  from { transform: rotate(0deg); }
  to   { transform: rotate(360deg); }
}
@media (prefers-reduced-motion: reduce) {
  .orbit-planet, .p-badge { animation: none; }
}
`;

const Home = () => {
  const { t, i18n } = useTranslation();
  const [statusState] = useContext(StatusContext);
  const actualTheme = useActualTheme();
  const [homePageContentLoaded, setHomePageContentLoaded] = useState(false);
  const [homePageContent, setHomePageContent] = useState('');
  const [noticeVisible, setNoticeVisible] = useState(false);
  const isMobile = useIsMobile();
  const isDemoSiteMode = statusState?.status?.demo_site_enabled || false;
  const serverAddress =
    statusState?.status?.server_address || `${window.location.origin}`;
  const endpointItems = API_ENDPOINTS.map((e) => ({ value: e }));
  const [endpointIndex, setEndpointIndex] = useState(0);
  const isChinese = i18n.language.startsWith('zh');

  /* 10 家国外供应商 → 行星：orbit 轨道 / r 半径% / rf 半径小数 / dur 公转周期 / delay 相位 */
  const PLANETS = [
    { id: 'openai', Icon: OpenAI, orbit: 0, r: 16, rf: 0.16, dur: 12, delay: 0 },
    { id: 'claude', Icon: Claude, orbit: 0, r: 21, rf: 0.21, dur: 16, delay: -4 },
    { id: 'gemini', Icon: Gemini, orbit: 0, r: 26, rf: 0.26, dur: 20, delay: -9 },
    { id: 'llama', Icon: Meta, orbit: 0, r: 31, rf: 0.31, dur: 24, delay: -14 },
    { id: 'mistral', Icon: Mistral, orbit: 1, r: 35, rf: 0.35, dur: 30, delay: -3 },
    { id: 'grok', Icon: Grok, orbit: 1, r: 39, rf: 0.39, dur: 36, delay: -12 },
    { id: 'aws', Icon: Aws, orbit: 1, r: 43, rf: 0.43, dur: 42, delay: -21 },
    { id: 'azure', Icon: AzureAI, orbit: 2, r: 46, rf: 0.46, dur: 52, delay: -6 },
    { id: 'nvidia', Icon: Nvidia, orbit: 2, r: 48, rf: 0.48, dur: 60, delay: -16 },
    { id: 'cohere', Icon: Cohere, orbit: 2, r: 50, rf: 0.5, dur: 70, delay: -28 },
  ];
  const ORBIT_RINGS = [0, 1, 2].map((i) => {
    const maxR = Math.max(...PLANETS.filter((p) => p.orbit === i).map((p) => p.r));
    return { key: i, radius: maxR + 4 };
  });

  const displayHomePageContent = async () => {
    setHomePageContent(localStorage.getItem('home_page_content') || '');
    const res = await API.get('/api/home_page_content');
    const { success, message, data } = res.data;
    if (success) {
      let content = data;
      if (!data.startsWith('https://')) {
        content = marked.parse(data);
      }
      setHomePageContent(content);
      localStorage.setItem('home_page_content', content);

      // 如果内容是 URL，则发送主题模式
      if (data.startsWith('https://')) {
        const iframe = document.querySelector('iframe');
        if (iframe) {
          iframe.onload = () => {
            iframe.contentWindow.postMessage({ themeMode: actualTheme }, '*');
            iframe.contentWindow.postMessage({ lang: i18n.language }, '*');
          };
        }
      }
    } else {
      showError(message);
      setHomePageContent('加载首页内容失败...');
    }
    setHomePageContentLoaded(true);
  };

  const handleCopyBaseURL = async () => {
    const ok = await copy(serverAddress);
    if (ok) {
      showSuccess(t('已复制到剪切板'));
    }
  };

  useEffect(() => {
    const checkNoticeAndShow = async () => {
      const lastCloseDate = localStorage.getItem('notice_close_date');
      const today = new Date().toDateString();
      if (lastCloseDate !== today) {
        try {
          const res = await API.get('/api/notice');
          const { success, data } = res.data;
          if (success && data && data.trim() !== '') {
            setNoticeVisible(true);
          }
        } catch (error) {
          console.error('获取公告失败:', error);
        }
      }
    };

    checkNoticeAndShow();
  }, []);

  useEffect(() => {
    displayHomePageContent().then();
  }, []);

  useEffect(() => {
    const timer = setInterval(() => {
      setEndpointIndex((prev) => (prev + 1) % endpointItems.length);
    }, 3000);
    return () => clearInterval(timer);
  }, [endpointItems.length]);

  return (
    <div className='w-full overflow-x-hidden'>
      <style>{SOLAR_CSS}</style>
      <NoticeModal
        visible={noticeVisible}
        onClose={() => setNoticeVisible(false)}
        isMobile={isMobile}
      />
      {homePageContentLoaded && homePageContent === '' ? (
        <div className='w-full overflow-x-hidden'>
          {/* Banner 部分 · 天体系统 */}
          <div className='solar-banner relative min-h-[500px] w-full overflow-x-hidden border-b border-semi-color-border md:min-h-[620px] lg:min-h-[720px]'>
            {/* 背景模糊晕染球 */}
            <div className='blur-ball blur-ball-indigo' />
            <div className='blur-ball blur-ball-teal' />

            {/* 深空星窗 */}
            <div
              className='relative flex flex-col items-center px-4 pt-16 pb-20 md:pt-20 md:pb-24 lg:pt-24 lg:pb-28'
              style={{
                background:
                  'radial-gradient(ellipse 90% 80% at 50% 42%, #101b36 0%, #0a1128 55%, rgba(10, 17, 40, 0.6) 80%, transparent 100%)',
              }}
            >
              {/* 天体系统 */}
              <div className='solar-viewport'>
                <div className='orbit-belt'>
                  {ORBIT_RINGS.map((ring) => (
                    <div
                      key={ring.key}
                      className='orbit-ring'
                      style={{
                        width: `${ring.radius * 2}%`,
                        height: `${ring.radius * 2}%`,
                      }}
                    />
                  ))}
                  {PLANETS.map((p) => (
                    <div
                      key={p.id}
                      className='orbit-planet'
                      style={{
                        '--r': `calc(var(--belt) * ${p.rf})`,
                        '--dur': `${p.dur}s`,
                        '--delay': `${p.delay}s`,
                      }}
                    >
                      <div className='p-arranger'>
                        <div className='p-badge'>
                          <p.Icon />
                        </div>
                      </div>
                    </div>
                  ))}
                </div>

                {/* 恒星 = heqiuyu */}
                <div className='solar-star'>
                  <div className='glow' />
                  <h1>heqiuyu</h1>
                </div>
              </div>

              {/* 副标语 */}
              <p className='solar-caption mt-8 text-base text-semi-color-text-1 md:mt-10 md:text-lg'>
                {t('国外主流大模型供应商，一个接口统一接入')}
              </p>

              {/* BASE URL 与端点选择 */}
              <div className='solar-caption mt-5 flex w-full max-w-md flex-col items-center justify-center gap-4 md:mt-6 md:flex-row'>
                <Input
                  readonly
                  value={serverAddress}
                  className='flex-1 !rounded-full'
                  size={isMobile ? 'default' : 'large'}
                  suffix={
                    <div className='flex items-center gap-2'>
                      <ScrollList
                        bodyHeight={32}
                        style={{ border: 'unset', boxShadow: 'unset' }}
                      >
                        <ScrollItem
                          mode='wheel'
                          cycled={true}
                          list={endpointItems}
                          selectedIndex={endpointIndex}
                          onSelect={({ index }) => setEndpointIndex(index)}
                        />
                      </ScrollList>
                      <Button
                        type='primary'
                        onClick={handleCopyBaseURL}
                        icon={<IconCopy />}
                        className='!rounded-full'
                      />
                    </div>
                  }
                />
              </div>

              {/* 操作按钮 */}
              <div className='solar-caption mt-8 flex flex-row items-center justify-center gap-4'>
                <Link to='/console'>
                  <Button
                    theme='solid'
                    type='primary'
                    size={isMobile ? 'default' : 'large'}
                    className='!rounded-3xl px-8 py-2'
                    icon={<IconPlay />}
                  >
                    {t('获取密钥')}
                  </Button>
                </Link>
                {isDemoSiteMode && statusState?.status?.version ? (
                  <Button
                    size={isMobile ? 'default' : 'large'}
                    className='flex items-center !rounded-3xl px-6 py-2'
                    icon={<IconGithubLogo />}
                    onClick={() =>
                      window.open(
                        'https://github.com/heqiuyu209/heqiuyu-api',
                        '_blank',
                      )
                    }
                  >
                    {statusState.status.version}
                  </Button>
                ) : (
                  <Link to='/docs'>
                    <Button
                      size={isMobile ? 'default' : 'large'}
                      className='flex items-center !rounded-3xl px-6 py-2'
                      icon={<IconFile />}
                    >
                      {t('文档')}
                    </Button>
                  </Link>
                )}
              </div>
            </div>
          </div>
        </div>
      ) : (
        <div className='w-full overflow-x-hidden'>
          {homePageContent.startsWith('https://') ? (
            <iframe
              src={homePageContent}
              className='h-screen w-full border-none'
            />
          ) : (
            <div
              className='mt-[60px]'
              dangerouslySetInnerHTML={{ __html: homePageContent }}
            />
          )}
        </div>
      )}
    </div>
  );
};

export default Home;
