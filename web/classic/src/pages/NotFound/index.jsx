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

import React from 'react';
import { Empty } from '@douyinfe/semi-ui';
import {
  IllustrationNotFound,
  IllustrationNotFoundDark,
} from '@douyinfe/semi-illustrations';
import { useTranslation } from 'react-i18next';

const NotFound = () => {
  const { t } = useTranslation();
  return (
    <div
      className='flex flex-col justify-center items-center h-screen p-8 relative overflow-hidden'
      style={{
        background:
          'radial-gradient(ellipse 45% 40% at 20% 15%, rgba(69,87,208,0.12), transparent 70%), radial-gradient(ellipse 40% 40% at 80% 85%, rgba(42,191,174,0.1), transparent 70%)',
      }}
    >
      <div
        aria-hidden
        className='text-[6rem] leading-none font-bold mb-2 opacity-90'
        style={{
          background:
            'linear-gradient(135deg, var(--space-primary), var(--space-accent), var(--space-violet))',
          WebkitBackgroundClip: 'text',
          WebkitTextFillColor: 'transparent',
          backgroundClip: 'text',
        }}
      >
        404
      </div>
      <Empty
        image={<IllustrationNotFound style={{ width: 250, height: 250 }} />}
        darkModeImage={
          <IllustrationNotFoundDark style={{ width: 250, height: 250 }} />
        }
        description={t('页面未找到，请检查您的浏览器地址是否正确')}
      />
    </div>
  );
};

export default NotFound;
